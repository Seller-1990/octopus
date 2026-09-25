package op

import (
	"context"
	"sort"
	"strings"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// SiteModelPriceCompareRow 价格对比视图的单行：一个站点/账号/分组对同一模型
// 的一份有效报价。原价（原始币种）与到手价（USD，含分组倍率与汇率）并存，
// 模型倍率单独展示（0=未知，由前端展示「—」）。
type SiteModelPriceCompareRow struct {
	QuoteID              int       `json:"quote_id"`
	SiteID               int       `json:"site_id"`
	SiteName             string    `json:"site_name"`
	SiteAccountID        *int      `json:"site_account_id,omitempty"`
	SiteAccountName      string    `json:"site_account_name"`
	GroupKey             string    `json:"group_key"`
	RouteCandidateID     *int      `json:"route_candidate_id,omitempty"`
	Currency             string    `json:"currency"`
	Input                float64   `json:"input"`
	Output               float64   `json:"output"`
	InputUSD             *float64  `json:"input_usd,omitempty"`
	OutputUSD            *float64  `json:"output_usd,omitempty"`
	ModelMultiplier      *float64  `json:"model_multiplier,omitempty"`
	GroupMultiplier      float64   `json:"group_multiplier"`
	GroupMultiplierKnown bool      `json:"group_multiplier_known"`
	Source               string    `json:"source"`
	ObservedAt           time.Time `json:"observed_at"`
	Stale                bool      `json:"stale"`
	ManualOverride       bool      `json:"manual_override"`
}

// SiteModelPriceCompareSummary 到手输出价的汇总统计。SpreadRatio =
// max/min（min>0 时才有值），≥2 提示「值得检查贵账号是否倍率错配」。
type SiteModelPriceCompareSummary struct {
	RowCount        int      `json:"row_count"`
	MinOutputUSD    *float64 `json:"min_output_usd,omitempty"`
	MedianOutputUSD *float64 `json:"median_output_usd,omitempty"`
	MaxOutputUSD    *float64 `json:"max_output_usd,omitempty"`
	SpreadRatio     *float64 `json:"spread_ratio,omitempty"`
}

// SiteModelPriceCompare 按模型名（大小写不敏感，与 matchingPriceQuotes 同一
// 口径）聚合各站点/账号/分组的有效报价。只取 status=valid 且 observed_at 在
// days 天内的行；折算复用 effectivePriceForCompare（分组倍率 × 汇率），与
// 路由打分共用同一权威语义，禁止展示层第二套折算。
func SiteModelPriceCompare(
	ctx context.Context,
	modelName string,
	days int,
	limit int,
) ([]SiteModelPriceCompareRow, SiteModelPriceCompareSummary, error) {
	summary := SiteModelPriceCompareSummary{}
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return nil, summary, nil
	}
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	var quotes []model.SiteModelPriceQuote
	err := dbpkg.GetDB().WithContext(ctx).
		Where("LOWER(model_name) = ?", name).
		Where("status = ?", model.PriceQuoteStatusValid).
		Where("observed_at > ?", cutoff).
		Order("observed_at DESC, id DESC").
		Limit(limit).
		Find(&quotes).Error
	if err != nil {
		return nil, summary, err
	}

	siteNames, accountNames, nameErr := compareDisplayNames(ctx, quotes)
	if nameErr != nil {
		return nil, summary, nameErr
	}
	now := time.Now()
	rows := make([]SiteModelPriceCompareRow, 0, len(quotes))
	outputUSDs := make([]float64, 0, len(quotes))
	for _, quote := range quotes {
		effective := effectivePriceFromQuote(ctx, quote, 0, quote.Source, !priceQuoteFresh(quote, now), "")
		row := SiteModelPriceCompareRow{
			QuoteID:              quote.ID,
			SiteID:               quote.SiteID,
			SiteName:             siteNames[quote.SiteID],
			SiteAccountID:        quote.SiteAccountID,
			SiteAccountName:      accountNameByID(accountNames, quote.SiteAccountID),
			GroupKey:             quote.GroupKey,
			RouteCandidateID:     quote.RouteCandidateID,
			Currency:             quote.Currency,
			Input:                quote.Input,
			Output:               quote.Output,
			ModelMultiplier:      nullableMultiplier(quote.ModelMultiplier),
			GroupMultiplier:      effective.GroupMultiplier,
			GroupMultiplierKnown: quote.GroupMultiplierKnown,
			Source:               string(quote.Source),
			ObservedAt:           quote.ObservedAt,
			Stale:                !priceQuoteFresh(quote, now),
			ManualOverride:       quote.ManualOverride,
		}
		if effective.Convertible && effective.ExchangeRateToUSD > 0 {
			inputUSD := effective.Input * effective.ExchangeRateToUSD
			outputUSD := effective.Output * effective.ExchangeRateToUSD
			row.InputUSD = &inputUSD
			row.OutputUSD = &outputUSD
			outputUSDs = append(outputUSDs, outputUSD)
		}
		rows = append(rows, row)
	}

	summary = buildPriceCompareSummary(rows, outputUSDs)
	return rows, summary, nil
}

func compareDisplayNames(
	ctx context.Context,
	quotes []model.SiteModelPriceQuote,
) (map[int]string, map[int]string, error) {
	siteIDs := make([]int, 0, len(quotes))
	accountIDs := make([]int, 0, len(quotes))
	seenSite := make(map[int]struct{}, len(quotes))
	seenAccount := make(map[int]struct{}, len(quotes))
	for _, quote := range quotes {
		if _, ok := seenSite[quote.SiteID]; !ok {
			seenSite[quote.SiteID] = struct{}{}
			siteIDs = append(siteIDs, quote.SiteID)
		}
		if quote.SiteAccountID != nil {
			id := *quote.SiteAccountID
			if _, ok := seenAccount[id]; !ok {
				seenAccount[id] = struct{}{}
				accountIDs = append(accountIDs, id)
			}
		}
	}
	siteNames := make(map[int]string, len(siteIDs))
	if len(siteIDs) > 0 {
		var sites []model.Site
		if err := dbpkg.GetDB().WithContext(ctx).
			Select("id", "name").
			Where("id IN ?", siteIDs).
			Find(&sites).Error; err != nil {
			return nil, nil, err
		}
		for _, site := range sites {
			siteNames[site.ID] = site.Name
		}
	}
	accountNames := make(map[int]string, len(accountIDs))
	if len(accountIDs) > 0 {
		var accounts []model.SiteAccount
		if err := dbpkg.GetDB().WithContext(ctx).
			Select("id", "name").
			Where("id IN ?", accountIDs).
			Find(&accounts).Error; err != nil {
			return nil, nil, err
		}
		for _, account := range accounts {
			accountNames[account.ID] = account.Name
		}
	}
	return siteNames, accountNames, nil
}

func accountNameByID(names map[int]string, accountID *int) string {
	if accountID == nil {
		return ""
	}
	return names[*accountID]
}

func nullableMultiplier(value float64) *float64 {
	if value <= 0 {
		return nil
	}
	copied := value
	return &copied
}

func buildPriceCompareSummary(
	rows []SiteModelPriceCompareRow,
	outputUSDs []float64,
) SiteModelPriceCompareSummary {
	summary := SiteModelPriceCompareSummary{RowCount: len(rows)}
	if len(outputUSDs) == 0 {
		return summary
	}
	sorted := append([]float64(nil), outputUSDs...)
	sort.Float64s(sorted)
	min := sorted[0]
	max := sorted[len(sorted)-1]
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	summary.MinOutputUSD = &min
	summary.MedianOutputUSD = &median
	summary.MaxOutputUSD = &max
	if min > 0 && max > min {
		spread := max / min
		summary.SpreadRatio = &spread
	}
	return summary
}
