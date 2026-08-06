package sitesync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const sub2APIRefreshWorkerLimit = 4

type Sub2APIRefreshSummary struct {
	Selected  int
	Succeeded int
	Failed    int
}

type sub2APIRefreshCandidate struct {
	site    *model.Site
	account *model.SiteAccount
}

var sub2APIRefreshPassRunning atomic.Bool

func RefreshDueSub2APIAccounts(ctx context.Context, workerLimit int) (Sub2APIRefreshSummary, error) {
	if err := ctx.Err(); err != nil {
		return Sub2APIRefreshSummary{}, err
	}
	if !sub2APIRefreshPassRunning.CompareAndSwap(false, true) {
		return Sub2APIRefreshSummary{}, nil
	}
	defer sub2APIRefreshPassRunning.Store(false)

	candidates, err := listSub2APIRefreshCandidates(ctx)
	if err != nil {
		return Sub2APIRefreshSummary{}, err
	}
	return refreshSub2APICandidates(ctx, candidates, workerLimit)
}

func listSub2APIRefreshCandidates(ctx context.Context) ([]sub2APIRefreshCandidate, error) {
	var accounts []model.SiteAccount
	if err := db.GetDB().WithContext(ctx).
		Select("id", "site_id", "access_token", "refresh_token", "token_expires_at", "credential_revision", "proxy_mode", "proxy_config_id", "verification_cookie_encrypted", "verification_user_agent", "verification_proxy_config_id", "verification_clash_node", "verification_expires_at", "preferred_clash_node", "enabled", "auto_sync").
		Where("enabled = ? AND auto_sync = ? AND refresh_token <> '' AND token_expires_at > 0", true, true).
		Find(&accounts).Error; err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, nil
	}

	siteIDs := make([]int, 0, len(accounts))
	seenSiteIDs := make(map[int]struct{}, len(accounts))
	for index := range accounts {
		if _, seen := seenSiteIDs[accounts[index].SiteID]; !seen {
			seenSiteIDs[accounts[index].SiteID] = struct{}{}
			siteIDs = append(siteIDs, accounts[index].SiteID)
		}
	}
	var sites []model.Site
	if err := db.GetDB().WithContext(ctx).
		Select("id", "base_url", "platform", "enabled", "proxy_mode", "proxy_config_id", "preferred_clash_node", "custom_header").
		Where("id IN ? AND enabled = ? AND platform = ?", siteIDs, true, model.SitePlatformSub2API).
		Find(&sites).Error; err != nil {
		return nil, err
	}
	sitesByID := make(map[int]*model.Site, len(sites))
	for index := range sites {
		sitesByID[sites[index].ID] = &sites[index]
	}

	result := make([]sub2APIRefreshCandidate, 0, len(accounts))
	for index := range accounts {
		siteRecord := sitesByID[accounts[index].SiteID]
		if siteRecord != nil && shouldProactivelyRefreshSub2API(&accounts[index]) {
			result = append(result, sub2APIRefreshCandidate{site: siteRecord, account: &accounts[index]})
		}
	}
	return result, nil
}

func refreshSub2APICandidates(ctx context.Context, candidates []sub2APIRefreshCandidate, workerLimit int) (Sub2APIRefreshSummary, error) {
	summary := Sub2APIRefreshSummary{}
	if len(candidates) == 0 {
		return summary, ctx.Err()
	}
	if workerLimit <= 0 {
		workerLimit = sub2APIRefreshWorkerLimit
	}
	workerLimit = min(workerLimit, len(candidates))
	jobs := make(chan sub2APIRefreshCandidate)
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	for range workerLimit {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				_, err := ensureFreshSub2APIAccessToken(ctx, candidate.site, candidate.account, false)
				recordSub2APIRefreshOutcome(ctx, candidate.account.ID, candidate.account.CredentialRevision, err)
				resultMu.Lock()
				if err != nil {
					summary.Failed++
				} else {
					summary.Succeeded++
				}
				resultMu.Unlock()
			}
		}()
	}
	for _, candidate := range candidates {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return summary, ctx.Err()
		case jobs <- candidate:
			summary.Selected++
		}
	}
	close(jobs)
	wg.Wait()
	return summary, ctx.Err()
}

func recordSub2APIRefreshOutcome(ctx context.Context, accountID int, credentialRevision int64, err error) {
	if accountID <= 0 {
		return
	}
	persistCtx := context.Background()
	if ctx != nil {
		persistCtx = context.WithoutCancel(ctx)
	}
	updates := map[string]any{"last_auth_failure_class": "", "last_auth_failure_at": nil}
	if err != nil {
		updates["last_auth_failure_class"] = classifySiteAuthFailure(err)
		updates["last_auth_failure_at"] = time.Now()
	}
	if updateErr := db.GetDB().WithContext(persistCtx).Model(&model.SiteAccount{}).
		Where("id = ? AND credential_revision = ?", accountID, credentialRevision).
		Updates(updates).Error; updateErr != nil {
		log.Warnf("persist sub2api refresh outcome failed (account=%d): %v", accountID, updateErr)
	}
}

func classifySiteAuthFailure(err error) string {
	if err == nil {
		return ""
	}
	if IsCloudflareProtectionError(err) {
		return "cloudflare_challenge"
	}
	if errors.Is(err, context.Canceled) {
		return "transport_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "transport_timeout"
	}
	message := strings.ToLower(err.Error())
	status := anyToInt64(apperror.Params(err)["statusCode"])
	switch {
	case status == 429:
		return "rate_limited"
	case status == 401 || strings.Contains(message, "invalid refresh") || strings.Contains(message, "refresh rejected"):
		return "refresh_rejected"
	case status == 403:
		return "permission_denied"
	case status >= 500 && status <= 599:
		return "upstream_5xx"
	case strings.Contains(message, "timeout") || strings.Contains(message, "connection"):
		return "transport_transient"
	default:
		return "contract_mismatch"
	}
}
