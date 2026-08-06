package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type PriceQuoteSource string

const (
	PriceQuoteSourceManualOverride PriceQuoteSource = "manual_override"
	PriceQuoteSourceSiteExact      PriceQuoteSource = "site_exact"
	PriceQuoteSourceSiteWide       PriceQuoteSource = "site_wide"
	PriceQuoteSourceSiteStale      PriceQuoteSource = "site_stale"
	PriceQuoteSourceGlobal         PriceQuoteSource = "global"
	PriceQuoteSourceUnknown        PriceQuoteSource = "unknown"
)

type PriceUnit string

const (
	PriceUnitPerMillionTokens PriceUnit = "per_million_tokens"
	PriceUnitPerRequest       PriceUnit = "per_request"
	PriceUnitSiteCredit       PriceUnit = "site_credit"
)

type PriceQuoteStatus string

const (
	PriceQuoteStatusValid    PriceQuoteStatus = "valid"
	PriceQuoteStatusRejected PriceQuoteStatus = "rejected"
)

type SiteModelPriceQuote struct {
	ID                   int              `json:"id" gorm:"primaryKey"`
	IdentityKey          string           `json:"identity_key" gorm:"size:64;not null;default:''"`
	RouteCandidateID     *int             `json:"route_candidate_id,omitempty" gorm:"index"`
	SiteID               int              `json:"site_id" gorm:"not null;index"`
	SiteAccountID        *int             `json:"site_account_id,omitempty" gorm:"index"`
	GroupKey             string           `json:"group_key" gorm:"size:128"`
	ModelName            string           `json:"model_name" gorm:"size:191;not null"`
	Source               PriceQuoteSource `json:"source" gorm:"type:varchar(32);not null"`
	Unit                 PriceUnit        `json:"unit" gorm:"type:varchar(32);not null;default:'per_million_tokens'"`
	Currency             string           `json:"currency" gorm:"size:16;not null;default:'USD'"`
	Input                float64          `json:"input"`
	Output               float64          `json:"output"`
	CacheRead            float64          `json:"cache_read"`
	CacheWrite           float64          `json:"cache_write"`
	PerRequest           float64          `json:"per_request"`
	ModelMultiplier      float64          `json:"model_multiplier" gorm:"not null;default:0"` // 模型倍率(如0.1x/1x)，0 表示未知
	GroupMultiplier      float64          `json:"group_multiplier" gorm:"not null;default:1"`
	GroupMultiplierKnown bool             `json:"-" gorm:"-"`
	ExchangeRateToUSD    float64          `json:"exchange_rate_to_usd" gorm:"not null"`
	RawPayload           string           `json:"raw_payload,omitempty" gorm:"type:text"`
	ObservedAt           time.Time        `json:"observed_at" gorm:"index"`
	ValidUntil           *time.Time       `json:"valid_until,omitempty"`
	ManualOverride       bool             `json:"manual_override" gorm:"not null;default:false"`
	Status               PriceQuoteStatus `json:"status" gorm:"type:varchar(16);not null;default:'valid';index"`
	LastError            string           `json:"last_error,omitempty" gorm:"type:text"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

func (q *SiteModelPriceQuote) RefreshIdentityKey() {
	if q == nil {
		return
	}
	accountID := 0
	if q.SiteAccountID != nil {
		accountID = *q.SiteAccountID
	}
	candidateID := 0
	if q.RouteCandidateID != nil {
		candidateID = *q.RouteCandidateID
	}
	payload := fmt.Sprintf(
		"site=%d|account=%d|group=%s|candidate=%d|model=%s|source=%s",
		q.SiteID,
		accountID,
		NormalizeSiteGroupKey(q.GroupKey),
		candidateID,
		strings.ToLower(strings.TrimSpace(q.ModelName)),
		q.Source,
	)
	sum := sha256.Sum256([]byte(payload))
	q.IdentityKey = hex.EncodeToString(sum[:])
}

type CurrencyRate struct {
	Currency  string    `json:"currency" gorm:"size:16;primaryKey"`
	RateToUSD float64   `json:"rate_to_usd" gorm:"not null"`
	UpdatedAt time.Time `json:"updated_at"`
}

type EffectivePrice struct {
	QuoteID           int              `json:"quote_id,omitempty"`
	RouteCandidateID  int              `json:"route_candidate_id,omitempty"`
	Source            PriceQuoteSource `json:"source"`
	Unit              PriceUnit        `json:"unit"`
	Currency          string           `json:"currency"`
	Input             float64          `json:"input"`
	Output            float64          `json:"output"`
	CacheRead         float64          `json:"cache_read"`
	CacheWrite        float64          `json:"cache_write"`
	PerRequest        float64          `json:"per_request"`
	GroupMultiplier   float64          `json:"group_multiplier"`
	ExchangeRateToUSD float64          `json:"exchange_rate_to_usd"`
	ObservedAt        *time.Time       `json:"observed_at,omitempty"`
	Stale             bool             `json:"stale"`
	Convertible       bool             `json:"convertible"`
	MatchReason       string           `json:"match_reason,omitempty"`
}

// CatalogPriceSummary is the effective site price used by the model catalog.
// CostUSD is populated only when the quote can be compared with other
// per-million-token prices.
type CatalogPriceSummary struct {
	CanonicalModelID  int                  `json:"canonical_model_id"`
	RouteCandidateID  int                  `json:"route_candidate_id"`
	SiteID            int                  `json:"site_id"`
	SiteName          string               `json:"site_name,omitempty"`
	SiteAccountID     *int                 `json:"site_account_id,omitempty"`
	SiteAccountName   string               `json:"site_account_name,omitempty"`
	GroupKey          string               `json:"group_key,omitempty"`
	UpstreamModelName string               `json:"upstream_model_name"`
	CandidateStatus   RouteCandidateStatus `json:"candidate_status"`
	Source            PriceQuoteSource     `json:"source"`
	Unit              PriceUnit            `json:"unit"`
	Currency          string               `json:"currency"`
	Input             float64              `json:"input"`
	Output            float64              `json:"output"`
	CacheRead         float64              `json:"cache_read"`
	CacheWrite        float64              `json:"cache_write"`
	PerRequest        float64              `json:"per_request"`
	GroupMultiplier   float64              `json:"group_multiplier"`
	ExchangeRateToUSD float64              `json:"exchange_rate_to_usd"`
	ObservedAt        *time.Time           `json:"observed_at,omitempty"`
	Stale             bool                 `json:"stale"`
	Convertible       bool                 `json:"convertible"`
	Comparable        bool                 `json:"comparable"`
	CostUSD           float64              `json:"cost_usd,omitempty"`
}

type CatalogPriceOverview struct {
	CanonicalModelID int                   `json:"canonical_model_id"`
	Best             *CatalogPriceSummary  `json:"best,omitempty"`
	Prices           []CatalogPriceSummary `json:"prices"`
}
