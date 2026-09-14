package sitesync

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

type syncSnapshot struct {
	accessToken        string
	credentialRevision int64
	persistCredential  bool
	credentialIsCookie bool
	groups             []model.SiteUserGroup
	tokens             []model.SiteToken
	models             []model.SiteModel
	groupResults       []siteGroupSyncResult
	status             model.SiteExecutionStatus
	balance            float64
	balanceUsed        float64
	todayIncome        float64
	// balanceObserved 标记本次同步是否真实观测到余额；失败时写回侧保留上一份（F09）
	balanceObserved bool
	message         string
	proxyMode       model.ProxyUsageMode
	proxyConfigID   *int
	clashNode       string
}

type siteBatchAccount struct {
	site    *model.Site
	account *model.SiteAccount
}

func SyncAccount(ctx context.Context, accountID int) (*model.SiteSyncResult, error) {
	return syncAccountInternal(ctx, accountID, true)
}

// syncAccountInternal：批量同步路径（fullCatalogSync=false）跳过每账号一次的
// 全局 CatalogSync，由 syncBatchAccounts 末尾统一执行一次（B#11）。
// EnforceMultiplierCap 与 ProjectAccount 保留在账号级：cap 语义依赖 persist 后
// 立即 enforce（两态化配套的用户拍板决策），投影必须消费 enforce 后的倍率。
func syncAccountInternal(ctx context.Context, accountID int, fullCatalogSync bool) (*model.SiteSyncResult, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}

	snapshot, syncErr := syncAccountStateWithRecovery(ctx, siteRecord, account)
	if snapshot == nil && syncErr != nil {
		message := sanitizeSiteStatusMessage(syncErr)
		updateErr := updateAccountSyncState(ctx, account.ID, account.CredentialRevision, model.SiteExecutionStatusFailed, message)
		if updateErr != nil {
			log.Warnf("failed to update site account sync state (account=%d): %v", account.ID, updateErr)
		} else if staleErr := MarkAccountProjectionStale(ctx, account.ID, message); staleErr != nil {
			log.Warnf("failed to mark site account projection stale (account=%d): %v", account.ID, staleErr)
		}
		return nil, sanitizeSiteError(syncErr)
	}
	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		return nil, sanitizeSiteError(err)
	}
	// 阶段 2 补充（阶段 4 第 15 条提前，用户拍板「提前 evaluate 两态化」的配套）：
	// 同步后无条件重算倍率策略——不依赖 pricing 刷新成功与否（sub2api 平台永无 pricing，
	// 否则 keep-block 保留的 known=false 行永远不被 EnforceMultiplierCap 处理、policy_blocked 卡死）。
	if _, _, err := op.EnforceMultiplierCap(ctx); err != nil {
		log.Warnf("enforce multiplier cap after sync failed (account=%d): %v", account.ID, err)
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		// F08 修复：persistSyncSnapshot 已写 last_sync_status=snapshot.status（可能 success），
		// 但 managed channel 投影失败——若继续返回，UI/调度会误以为「同步成功」而路由仍是旧值。
		// 这里显式把状态纠正为 failed + 标记投影 stale（复用 fetch 失败路径的同一机制）。
		// 前缀不带「同步成功」断言（snapshot.status 本身可能是 failed，P2 修正避免误导）。
		projectionMessage := sanitizeSiteStatusText("站点快照已保存但渠道投影失败：" + sanitizeSiteStatusMessage(err))
		if updateErr := updateAccountSyncState(ctx, account.ID, snapshot.credentialRevision, model.SiteExecutionStatusFailed, projectionMessage); updateErr != nil {
			log.Warnf("failed to persist projection failure state (account=%d): %v", account.ID, updateErr)
		}
		if staleErr := MarkAccountProjectionStale(ctx, account.ID, projectionMessage); staleErr != nil {
			log.Warnf("failed to mark account projection stale (account=%d): %v", account.ID, staleErr)
		}
		return nil, sanitizeSiteError(err)
	}

	// 同步成功后对账签到状态：今天的签到如有失败记录，立即补一次签到。
	// 根因：签到失败后退避最长 72 小时（buildNextRandomCheckinAt），期间
	// 同步可以反复成功——面板于是整天显示「同步完成但签到状态异常」。
	// 同步成功本身证明凭据与会话当前可用，此时补签是安全的；补签结果经
	// checkinAccount 正常写回 last_checkin_* 与退避计划，失败不影响同步结果。
	if snapshot.status == model.SiteExecutionStatusSuccess || snapshot.status == model.SiteExecutionStatusPartial {
		reconcileCheckinAfterSync(ctx, account.ID)
	}

	var catalogErr error
	if fullCatalogSync {
		_, catalogErr = op.CatalogSync(ctx)
	}

	if catalogErr == nil {
		pricingAccount := *account
		pricingAccount.ProxyMode = snapshot.proxyMode
		pricingAccount.ProxyConfigID = cloneInt(snapshot.proxyConfigID)
		pricingAccount.PreferredClashNode = snapshot.clashNode
		if priceErr := refreshSitePricingQuotes(
			ctx,
			siteRecord,
			&pricingAccount,
			snapshot.accessToken,
		); priceErr != nil {
			log.Debugf("site pricing refresh skipped (account=%d): %v", account.ID, sanitizeSiteError(priceErr))
		}
	}

	modelNames := make([]string, 0, len(snapshot.models))
	for _, item := range snapshot.models {
		modelNames = append(modelNames, item.ModelName)
	}
	slices.Sort(modelNames)

	result := &model.SiteSyncResult{
		AccountID:       account.ID,
		SiteID:          siteRecord.ID,
		Status:          snapshot.status,
		ChannelCount:    len(channelIDs),
		GroupCount:      len(snapshot.groups),
		TokenCount:      len(snapshot.tokens),
		ModelCount:      len(snapshot.models),
		ManagedChannels: channelIDs,
		Models:          modelNames,
		GroupResults:    exportSiteSyncGroupResults(snapshot.groupResults),
		Message:         sanitizeSiteStatusText(snapshot.message),
	}
	if catalogErr != nil {
		message := sanitizeSiteStatusText(
			result.Message + "；模型目录投影失败：" + sanitizeSiteStatusMessage(catalogErr),
		)
		if result.Status == model.SiteExecutionStatusSuccess {
			result.Status = model.SiteExecutionStatusPartial
		}
		result.Message = message
		if updateErr := updateAccountSyncState(ctx, account.ID, snapshot.credentialRevision, result.Status, message); updateErr != nil {
			log.Warnf("failed to persist partial catalog sync state (account=%d): %v", account.ID, updateErr)
		}
		return result, sanitizeSiteError(catalogErr)
	}
	if syncErr != nil {
		return result, sanitizeSiteError(syncErr)
	}
	return result, nil
}

func CheckinAccount(ctx context.Context, accountID int) (*model.SiteCheckinResult, error) {
	return checkinAccount(ctx, accountID, "manual")
}

func checkinAccount(ctx context.Context, accountID int, source string) (*model.SiteCheckinResult, error) {
	startedAt := time.Now()
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}

	result, resolvedAccessToken, err := checkinAccountStateWithRecovery(ctx, siteRecord, account)
	if err != nil {
		status := model.SiteExecutionStatusFailed
		lowered := strings.ToLower(err.Error())
		if strings.Contains(lowered, "not supported") || strings.Contains(lowered, "not found") {
			status = model.SiteExecutionStatusSkipped
		}
		message := sanitizeSiteStatusMessage(err)
		updateErr := updateAccountCheckinState(ctx, account, status, message, false, resolvedAccessToken)
		if updateErr != nil {
			return nil, sanitizeSiteError(updateErr)
		}
		persistCheckinLog(ctx, siteRecord.ID, account.ID, status, message, "", source, time.Since(startedAt))
		return &model.SiteCheckinResult{AccountID: account.ID, SiteID: siteRecord.ID, Status: status, Message: message}, nil
	}

	result.AccountID = account.ID
	result.SiteID = siteRecord.ID
	result.Message = sanitizeSiteStatusText(result.Message)
	if err := updateAccountCheckinState(ctx, account, result.Status, result.Message, result.Status == model.SiteExecutionStatusSuccess, resolvedAccessToken); err != nil {
		return nil, sanitizeSiteError(err)
	}
	persistCheckinLog(ctx, siteRecord.ID, account.ID, result.Status, result.Message, result.Reward, source, time.Since(startedAt))
	return result, nil
}

func persistCheckinLog(ctx context.Context, siteID, accountID int, status model.SiteExecutionStatus, message, reward, source string, elapsed time.Duration) {
	logEntry := model.SiteCheckinLog{
		SiteID:        siteID,
		SiteAccountID: accountID,
		Status:        status,
		Message:       message,
		Reward:        reward,
		LatencyMS:     elapsed.Milliseconds(),
		Source:        source,
	}
	if err := op.SiteCheckinLogAdd(ctx, logEntry); err != nil {
		log.Warnf("failed to persist checkin log (account=%d): %v", accountID, err)
	}
}

// 全量同步/签到的上次执行时间（含定时与手动触发），仅内存记录，重启后清零
var (
	lastBatchTimeMu    sync.RWMutex
	lastSyncAllTime    time.Time
	lastCheckinAllTime time.Time
)

func markLastSyncAllTime() {
	lastBatchTimeMu.Lock()
	lastSyncAllTime = time.Now()
	lastBatchTimeMu.Unlock()
}

func markLastCheckinAllTime() {
	lastBatchTimeMu.Lock()
	lastCheckinAllTime = time.Now()
	lastBatchTimeMu.Unlock()
}

func LastSyncAllTime() time.Time {
	lastBatchTimeMu.RLock()
	defer lastBatchTimeMu.RUnlock()
	return lastSyncAllTime
}

func LastCheckinAllTime() time.Time {
	lastBatchTimeMu.RLock()
	defer lastBatchTimeMu.RUnlock()
	return lastCheckinAllTime
}

func SyncAll(ctx context.Context) {
	SyncAllWithOptions(ctx, SiteBatchOptions{Trigger: SiteBatchTriggerScheduled})
}

func SyncAllWithOptions(ctx context.Context, opts SiteBatchOptions) SiteBatchSummary {
	trigger := normalizedSiteBatchTrigger(opts.Trigger)
	sites, err := op.SiteList(ctx)
	if err != nil {
		log.Warnw("sitesync.sync.list_failed", "trigger", string(trigger), "reason", string(siteBatchReason(err)), "message", sanitizeSiteStatusMessage(err))
		return SiteBatchSummary{Phase: SiteBatchPhaseSync, Trigger: trigger}
	}
	defer markLastSyncAllTime()
	return syncBatchAccounts(ctx, eligibleSyncAccounts(sites), opts)
}

func SyncAccountsWithOptions(ctx context.Context, accountIDs []int, opts SiteBatchOptions) SiteBatchSummary {
	items := make([]siteBatchAccount, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		siteRecord, account, err := loadSiteAccount(ctx, accountID)
		if err != nil {
			log.Debugf("site import sync account load failed (account=%d): %v", accountID, sanitizeSiteStatusMessage(err))
			continue
		}
		if siteRecord == nil || account == nil || !siteRecord.Enabled || !account.Enabled {
			continue
		}
		items = append(items, siteBatchAccount{site: siteRecord, account: account})
	}
	return syncBatchAccounts(ctx, items, opts)
}

func syncBatchAccounts(ctx context.Context, items []siteBatchAccount, opts SiteBatchOptions) SiteBatchSummary {
	// Site batch logging intentionally aggregates account-level business failures.
	// Individual account messages are stored on the account status; console logs
	// stay aggregated to avoid leaking upstream HTML and overwhelming operators.
	summary := newSiteBatchSummary(SiteBatchPhaseSync, opts, len(items))
	defer summary.emitLog()
	sawSyncedAccount := false
	for i := 0; i < len(items); i++ {
		item := items[i]
		if !waitSiteBatchInterval(ctx, 500*time.Millisecond) {
			summary.markCanceled(ctx.Err())
			recordBatchCanceledSkips(summary, items[i:])
			return *summary
		}
		result, err := syncAccountInternal(ctx, item.account.ID, false)
		if err != nil {
			summary.recordFailure(item.site.ID, item.site.Platform, item.account.ID, err)
			if IsCloudflareProtectionError(err) || siteBatchReason(err) == SiteBatchReasonCloudflareProtection {
				i = recordCloudflareSkipsAndWait(ctx, summary, items, i, CloudflareRetryAfter(err))
			}
			continue
		}
		sawSyncedAccount = true
		summary.recordResult(item.site.ID, item.site.Platform, item.account.ID, result.Status, result.Message)
	}
	// B#11：全局 CatalogSync 从每账号一次收敛为批量末尾一次。任一账号完整
	// 走完同步+投影才需要重建目录；全部失败时无需空跑。
	if sawSyncedAccount {
		if _, err := op.CatalogSync(ctx); err != nil {
			log.Warnf("batch catalog sync after site sync failed: %v", err)
		}
	}
	return *summary
}

func CheckinAll(ctx context.Context) {
	CheckinAllWithOptions(ctx, SiteBatchOptions{Trigger: SiteBatchTriggerScheduled})
}

func CheckinAllWithOptions(ctx context.Context, opts SiteBatchOptions) SiteBatchSummary {
	trigger := normalizedSiteBatchTrigger(opts.Trigger)
	sites, err := op.SiteList(ctx)
	if err != nil {
		log.Warnw("sitesync.checkin.list_failed", "trigger", string(trigger), "reason", string(siteBatchReason(err)), "message", sanitizeSiteStatusMessage(err))
		return SiteBatchSummary{Phase: SiteBatchPhaseCheckin, Trigger: trigger}
	}
	defer markLastCheckinAllTime()
	items := eligibleCheckinAccounts(sites)
	summary := newSiteBatchSummary(SiteBatchPhaseCheckin, opts, len(items))
	defer summary.emitLog()
	now := time.Now()
	for i := 0; i < len(items); i++ {
		item := items[i]
		// 防重签护栏：非手动触发时，当天已成功签到的账号不再重签。
		// 根因：签到任务 runOnStart=true，应用每次重启/更新都会立即重跑全量签到；
		// 此前只有随机调度账号有到期护栏，固定间隔账号会被无条件重签——
		// 重复请求被站点拒绝/风控返回未知错误后，last_checkin_status 被覆写为
		// failed，造成"白天 1 个失败、重启后变成 11 个"的当天状态突变。
		// 手动触发不受限，用户仍可强制重签。
		if trigger != SiteBatchTriggerManual &&
			item.account.LastCheckinAt != nil && !item.account.LastCheckinAt.IsZero() &&
			isSameLocalDay(*item.account.LastCheckinAt, now) &&
			item.account.LastCheckinStatus == model.SiteExecutionStatusSuccess {
			summary.recordSkip(item.site.ID, item.site.Platform, SiteBatchReasonAlreadyCheckedInToday, 1)
			continue
		}
		if item.account.RandomCheckin {
			nextAt, scheduleErr := ensureRandomCheckinSchedule(ctx, item.account, now)
			if scheduleErr != nil {
				summary.recordFailure(item.site.ID, item.site.Platform, item.account.ID, sanitizeSiteError(scheduleErr))
				continue
			}
			if nextAt != nil && now.Before(*nextAt) {
				summary.recordSkip(item.site.ID, item.site.Platform, SiteBatchReasonScheduledLater, 1)
				continue
			}
		}
		if !waitSiteBatchInterval(ctx, 500*time.Millisecond) {
			summary.markCanceled(ctx.Err())
			recordBatchCanceledSkips(summary, items[i:])
			return *summary
		}
		result, err := checkinAccount(ctx, item.account.ID, string(trigger))
		if err != nil {
			summary.recordFailure(item.site.ID, item.site.Platform, item.account.ID, err)
			if IsCloudflareProtectionError(err) || siteBatchReason(err) == SiteBatchReasonCloudflareProtection {
				i = recordCloudflareSkipsAndWait(ctx, summary, items, i, CloudflareRetryAfter(err))
			}
			continue
		}
		if result.Status == model.SiteExecutionStatusSkipped {
			summary.recordSkip(item.site.ID, item.site.Platform, SiteBatchReasonUnsupportedCheckin, 1)
			continue
		}
		summary.recordResult(item.site.ID, item.site.Platform, item.account.ID, result.Status, result.Message)
	}
	return *summary
}

func eligibleSyncAccounts(sites []model.Site) []siteBatchAccount {
	items := make([]siteBatchAccount, 0)
	for siteIndex := range sites {
		siteRecord := &sites[siteIndex]
		if !siteRecord.Enabled {
			continue
		}
		for accountIndex := range siteRecord.Accounts {
			account := &siteRecord.Accounts[accountIndex]
			if !account.Enabled || !account.AutoSync {
				continue
			}
			items = append(items, siteBatchAccount{site: siteRecord, account: account})
		}
	}
	return items
}

func eligibleCheckinAccounts(sites []model.Site) []siteBatchAccount {
	items := make([]siteBatchAccount, 0)
	for siteIndex := range sites {
		siteRecord := &sites[siteIndex]
		if !siteRecord.Enabled {
			continue
		}
		for accountIndex := range siteRecord.Accounts {
			account := &siteRecord.Accounts[accountIndex]
			if !account.Enabled || !account.AutoCheckin {
				continue
			}
			items = append(items, siteBatchAccount{site: siteRecord, account: account})
		}
	}
	return items
}

// isSameLocalDay 判断两个时间是否属于同一本地自然日（与前端 happenedToday 口径一致）。
func isSameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

// checkinReconcileMinInterval 补签与上一次签到尝试的最小间隔：同步成功可以
// 触发补签，但不能跟着同步节奏对失败站点高频重试（上游风控风险）。
const checkinReconcileMinInterval = 30 * time.Minute

// checkinReconcileMaxFailStreak 连续失败达到该次数后停止同步触发补签，交还
// 正常退避调度（CheckinAll）：站点侧持续拒绝（改版/风控）时不无限重试。
const checkinReconcileMaxFailStreak = 3

// reconcileCheckinAfterSync 在同步成功后对账签到状态。门禁基于**重读**的
// 账号当前状态：同步耗时期间账号可能已被手动签到或调度器更新，用同步
// 开始时的陈旧快照判断会误触发补签并覆盖并发的当日成功。
func reconcileCheckinAfterSync(ctx context.Context, accountID int) {
	fresh, err := op.SiteAccountGet(accountID, ctx)
	if err != nil || fresh == nil {
		return
	}
	if !shouldReconcileCheckinAfterSync(fresh) {
		return
	}
	if _, err := checkinAccount(ctx, accountID, "sync"); err != nil {
		log.Warnf("post-sync checkin reconcile failed (account=%d): %v", accountID, sanitizeSiteError(err))
	}
}

// shouldReconcileCheckinAfterSync 判断是否需要补签：仅针对存在真实失败记录
// （failed）且开启了自动签到的账号，并施加两道节流——
// ① 距上次签到尝试不足 checkinReconcileMinInterval 不补（防与手动/定时
//
//	签到互相放大）；
//
// ② 连续失败达 checkinReconcileMaxFailStreak 后不再补（交还正常退避调度，
//
//	避免按同步频率无限重试）。
//
// idle（从未签到）走正常调度；skipped（平台不支持）与 success 不触发。
func shouldReconcileCheckinAfterSync(account *model.SiteAccount) bool {
	if account == nil || !account.Enabled || !account.AutoCheckin {
		return false
	}
	// 只补真实失败：idle 走正常调度，skipped（平台不支持）与 success 无需重试
	if account.LastCheckinStatus != model.SiteExecutionStatusFailed {
		return false
	}
	now := time.Now()
	if account.LastCheckinAt != nil && !account.LastCheckinAt.IsZero() &&
		now.Sub(*account.LastCheckinAt) < checkinReconcileMinInterval {
		return false
	}
	return account.CheckinFailStreak < checkinReconcileMaxFailStreak
}

func recordCloudflareSkipsAndWait(ctx context.Context, summary *SiteBatchSummary, items []siteBatchAccount, currentIndex int, retryAfter time.Duration) int {
	current := items[currentIndex]
	lastSkipped := currentIndex
	for j := currentIndex + 1; j < len(items); j++ {
		if items[j].site.ID != current.site.ID {
			break
		}
		summary.recordSkip(items[j].site.ID, items[j].site.Platform, SiteBatchReasonCloudflareProtection, 1)
		lastSkipped = j
	}
	waitSiteCloudflareRetryAfter(ctx, retryAfter)
	return lastSkipped
}

func recordBatchCanceledSkips(summary *SiteBatchSummary, items []siteBatchAccount) {
	for _, item := range items {
		summary.recordSkip(item.site.ID, item.site.Platform, SiteBatchReasonBatchCanceled, 1)
	}
}

func waitSiteBatchInterval(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func waitSiteCloudflareRetryAfter(ctx context.Context, retryAfter time.Duration) {
	waitSiteBatchInterval(ctx, retryAfter)
}

func DeleteSite(ctx context.Context, siteID int) error {
	siteRecord, err := op.SiteGet(siteID, ctx)
	if err != nil {
		return err
	}
	for _, account := range siteRecord.Accounts {
		if err := deleteManagedChannelsByAccount(ctx, account.ID); err != nil {
			return err
		}
	}
	return op.SiteDel(siteID, ctx)
}

func ArchiveSite(ctx context.Context, siteID int) error {
	siteRecord, err := op.SiteGet(siteID, ctx)
	if err != nil {
		return err
	}
	for _, account := range siteRecord.Accounts {
		if err := deleteManagedChannelsByAccount(ctx, account.ID); err != nil {
			return err
		}
	}
	return op.SiteArchive(siteID, ctx)
}

func RestoreSite(ctx context.Context, siteID int) error {
	return op.SiteRestore(siteID, ctx)
}

func ListArchivedSites(ctx context.Context) ([]model.Site, error) {
	return op.SiteListArchived(ctx)
}

func DeleteSiteAccount(ctx context.Context, accountID int) error {
	if err := deleteManagedChannelsByAccount(ctx, accountID); err != nil {
		return err
	}
	return op.SiteAccountDel(accountID, ctx)
}
