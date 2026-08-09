package sitesync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

const newAPIBaseUSDPerMillionTokens = 2.0

func refreshSitePricingQuotes(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
) error {
	if siteRecord == nil || account == nil || !managedSessionRequestAvailable(ctx, accessToken) {
		return nil
	}
	payload, err := requestJSONWithManagedAccessToken(
		ctx,
		siteRecord,
		"GET",
		buildSiteURL(siteRecord.BaseURL, "/api/pricing"),
		nil,
		accessToken,
		account,
	)
	if err != nil {
		if markErr := op.SiteModelPriceQuoteMarkRefreshError(ctx, siteRecord.ID, account.ID, err); markErr != nil {
			return fmt.Errorf("%w (record refresh error: %v)", err, markErr)
		}
		return err
	}
	if err := op.SiteUserGroupMultipliersUpdate(ctx, account.ID, parseSitePricingGroupMultipliers(payload)); err != nil {
		if markErr := op.SiteModelPriceQuoteMarkRefreshError(ctx, siteRecord.ID, account.ID, err); markErr != nil {
			return fmt.Errorf("%w (record refresh error: %v)", err, markErr)
		}
		return err
	}
	// Pricing can change the independent multiplier policy after the initial
	// projection pass. Reconcile managed channels now so a newly blocked group
	// is disabled immediately and a recovered group is re-enabled without
	// waiting for the next full sync.
	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		if markErr := op.SiteModelPriceQuoteMarkRefreshError(ctx, siteRecord.ID, account.ID, err); markErr != nil {
			return fmt.Errorf("%w (record projection refresh error: %v)", err, markErr)
		}
		return err
	}
	quotes := parseSitePricingQuotes(siteRecord.ID, account.ID, payload)
	if err := op.SiteModelPriceQuotesUpsert(ctx, quotes); err != nil {
		if markErr := op.SiteModelPriceQuoteMarkRefreshError(ctx, siteRecord.ID, account.ID, err); markErr != nil {
			return fmt.Errorf("%w (record refresh error: %v)", err, markErr)
		}
		return err
	}
	return op.SiteModelPriceQuoteClearRefreshError(ctx, siteRecord.ID, account.ID)
}

func parseSitePricingQuotes(siteID, accountID int, payload map[string]any) []model.SiteModelPriceQuote {
	if siteID <= 0 || accountID <= 0 || payload == nil {
		return nil
	}
	items := sitePricingItems(payload)
	groupRatios := parseSitePricingGroupMultipliers(payload)
	defaultCurrency := firstNonEmptyString(
		jsonString(payload["currency"]),
		jsonString(payload["price_currency"]),
		jsonString(nestedValue(payload, "data", "currency")),
		jsonString(nestedValue(payload, "data", "price_currency")),
	)
	defaultUnit := firstNonEmptyString(
		jsonString(payload["unit"]),
		jsonString(payload["price_unit"]),
		jsonString(nestedValue(payload, "data", "unit")),
		jsonString(nestedValue(payload, "data", "price_unit")),
	)
	now := time.Now()
	result := make([]model.SiteModelPriceQuote, 0, len(items))
	for _, item := range items {
		modelName := firstNonEmptyString(
			jsonString(item["model_name"]),
			jsonString(item["model"]),
			jsonString(item["name"]),
			jsonString(item["id"]),
		)
		if strings.TrimSpace(modelName) == "" {
			continue
		}
		groups := normalizeStringList(item["enable_groups"])
		hasExplicitGroups := len(groups) > 0
		if len(groups) == 0 {
			groups = []string{model.SiteDefaultGroupKey}
		}
		raw, _ := json.Marshal(item)
		quotaType := int(jsonFloat(item["quota_type"]))
		direct := nestedPricingMap(item["token_price_usd_per_million"])
		inputDirect := firstNonZero(
			jsonFloat(direct["input"]),
			jsonFloat(item["input_price"]),
			jsonFloat(item["input"]),
		)
		outputDirect := firstNonZero(
			jsonFloat(direct["output"]),
			jsonFloat(item["output_price"]),
			jsonFloat(item["output"]),
		)
		cacheReadDirect := firstNonZero(
			jsonFloat(direct["cache_read"]),
			jsonFloat(item["cache_read_price"]),
		)
		cacheWriteDirect := firstNonZero(
			jsonFloat(direct["cache_write"]),
			jsonFloat(item["cache_write_price"]),
		)
		modelRatio := jsonFloat(item["model_ratio"])
		completionRatio := jsonFloat(item["completion_ratio"])
		if completionRatio == 0 {
			completionRatio = 1
		}
		unit := normalizeSitePriceUnit(firstNonEmptyString(
			jsonString(item["unit"]),
			jsonString(item["price_unit"]),
			jsonString(item["billing_unit"]),
			jsonString(direct["unit"]),
			defaultUnit,
		))
		if quotaType == 1 {
			unit = model.PriceUnitPerRequest
		}
		currency := normalizeSitePriceCurrency(firstNonEmptyString(
			jsonString(item["currency"]),
			jsonString(item["price_currency"]),
			jsonString(direct["currency"]),
			defaultCurrency,
		))
		if currency == "" && len(direct) > 0 {
			currency = "USD"
		}
		if unit == model.PriceUnitSiteCredit && currency == "" {
			currency = "SITE_CREDIT"
		}
		if currency == "" {
			currency = "USD"
		}

		for _, group := range groups {
			groupKey := model.NormalizeSiteGroupKey(group)
			// X9 ok-check：隐式 default 组（enable_groups 为空）不在 map 时按 {1, false}，不依赖 S3 补 1。
			gv, ok := groupRatios[groupKey]
			if !ok {
				gv = op.GroupMultiplierValue{Value: 1}
			}
			source := model.PriceQuoteSourceSiteWide
			if hasExplicitGroups {
				source = model.PriceQuoteSourceSiteExact
			}
			quote := model.SiteModelPriceQuote{
				SiteID:               siteID,
				SiteAccountID:        intPointer(accountID),
				GroupKey:             groupKey,
				ModelName:            strings.TrimSpace(modelName),
				Source:               source,
				Unit:                 unit,
				Currency:             currency,
				ModelMultiplier:      modelRatio,
				GroupMultiplier:      gv.Value,
				GroupMultiplierKnown: gv.Known,
				RawPayload:           string(raw),
				ObservedAt:           now,
			}
			if currency == "USD" {
				quote.ExchangeRateToUSD = 1
			}
			if quotaType == 1 {
				quote.PerRequest = parsePerRequestPrice(item["model_price"])
			} else {
				quote.Input = inputDirect
				quote.Output = outputDirect
				quote.CacheRead = cacheReadDirect
				quote.CacheWrite = cacheWriteDirect
				if quote.Input == 0 && modelRatio != 0 {
					quote.Input = modelRatio * newAPIBaseUSDPerMillionTokens
				}
				if quote.Output == 0 && quote.Input != 0 {
					quote.Output = quote.Input * completionRatio
				}
			}
			if quote.Input == 0 && quote.Output == 0 && quote.PerRequest == 0 {
				continue
			}
			result = append(result, quote)
		}
	}
	return result
}

func parseSitePricingGroupMultipliers(payload map[string]any) map[string]op.GroupMultiplierValue {
	result := make(map[string]op.GroupMultiplierValue)
	if payload == nil {
		return result
	}
	// S2 修正：known 只来自原始 group_ratio（不用补后 map 反推，修复坏标记）
	for key, v := range parsePricingGroupRatios(payload["group_ratio"]) {
		result[model.NormalizeSiteGroupKey(key)] = op.GroupMultiplierValue{Value: v, Known: true}
	}
	if len(result) == 0 {
		for key, v := range parsePricingGroupRatios(nestedValue(payload, "data", "group_ratio")) {
			result[model.NormalizeSiteGroupKey(key)] = op.GroupMultiplierValue{Value: v, Known: true}
		}
	}
	// New API omits default 1x groups from group_ratio but still lists them in enable_groups.
	// 修订 5：无条件执行（删除原 211-213 提前返回），补 1x 标 known=false（暂定）。
	for _, item := range sitePricingItems(payload) {
		for _, group := range normalizeStringList(item["enable_groups"]) {
			groupKey := model.NormalizeSiteGroupKey(group)
			if _, configured := result[groupKey]; !configured {
				result[groupKey] = op.GroupMultiplierValue{Value: 1, Known: false}
			}
		}
	}
	return result
}

func sitePricingItems(payload map[string]any) []map[string]any {
	items := normalizeItemSlice(payload["data"])
	if len(items) == 0 {
		items = normalizeItemSlice(nestedValue(payload, "data", "items"))
	}
	return items
}

func parsePricingGroupRatios(value any) map[string]float64 {
	result := make(map[string]float64)
	raw, ok := value.(map[string]any)
	if !ok {
		return result
	}
	for key, item := range raw {
		if multiplier := parseOptionalSiteGroupMultiplier(item); multiplier != nil {
			result[model.NormalizeSiteGroupKey(key)] = *multiplier
		}
	}
	return result
}

func nestedPricingMap(value any) map[string]any {
	if item, ok := value.(map[string]any); ok {
		return item
	}
	return map[string]any{}
}

func parsePerRequestPrice(value any) float64 {
	if direct := jsonFloat(value); direct != 0 {
		return direct
	}
	if item, ok := value.(map[string]any); ok {
		return jsonFloat(item["input"]) + jsonFloat(item["output"])
	}
	return 0
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func normalizeSitePriceUnit(value string) model.PriceUnit {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "per_million_tokens", "per-million-tokens", "per_1m_tokens", "1m_tokens", "token":
		return model.PriceUnitPerMillionTokens
	case "per_request", "per-request", "request":
		return model.PriceUnitPerRequest
	case "site_credit", "site-credit", "credit", "credits", "quota":
		return model.PriceUnitSiteCredit
	default:
		return model.PriceUnit(strings.TrimSpace(value))
	}
}

func normalizeSitePriceCurrency(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "CREDIT", "CREDITS", "QUOTA", "SITE-CREDIT", "SITE_CREDIT":
		return "SITE_CREDIT"
	default:
		return strings.ToUpper(strings.TrimSpace(value))
	}
}

func intPointer(value int) *int {
	return &value
}
