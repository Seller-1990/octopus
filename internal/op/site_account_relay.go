package op

import (
	"context"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// PropagateAccountRelaySettings 将账号级的 TLS 指纹与 User-Agent 同步到
// 该账号绑定的全部托管渠道。同步失败只记录告警，不阻断账号更新。
func PropagateAccountRelaySettings(ctx context.Context, account *model.SiteAccount) error {
	if account == nil {
		return nil
	}
	site, err := SiteGet(account.SiteID, ctx)
	if err != nil {
		return err
	}
	headers := BuildAccountRelayHeaders(site.CustomHeader, account.UserAgent)

	var bindings []model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).
		Where("site_account_id = ?", account.ID).
		Find(&bindings).Error; err != nil {
		return err
	}

	for _, binding := range bindings {
		channel, err := ChannelGet(binding.ChannelID, ctx)
		if err != nil {
			log.Warnf("failed to load bound channel %d for account %d: %v", binding.ChannelID, account.ID, err)
			continue
		}
		fingerprint := account.TLSFingerprint
		req := &model.ChannelUpdateRequest{
			ID:                 channel.ID,
			TLSFingerprint:     &fingerprint,
			CustomHeader:       &headers,
			BypassManagedCheck: true,
		}
		if _, err := ChannelUpdate(req, ctx); err != nil {
			log.Warnf("failed to propagate relay settings to channel %d: %v", channel.ID, err)
		}
	}
	return nil
}

// BuildAccountRelayHeaders 以站点 Header 为基础，叠加账号级 User-Agent。
// 账号未设置 UA 时保持站点 Header 原样。
func BuildAccountRelayHeaders(siteHeaders []model.CustomHeader, userAgent string) []model.CustomHeader {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return append([]model.CustomHeader(nil), siteHeaders...)
	}

	headers := make([]model.CustomHeader, 0, len(siteHeaders)+1)
	for _, header := range siteHeaders {
		if strings.EqualFold(strings.TrimSpace(header.HeaderKey), "User-Agent") {
			continue
		}
		headers = append(headers, header)
	}
	return append(headers, model.CustomHeader{HeaderKey: "User-Agent", HeaderValue: userAgent})
}
