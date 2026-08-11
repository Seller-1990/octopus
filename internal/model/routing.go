package model

import "time"

type ProtocolName string

const (
	ProtocolOpenAIChat      ProtocolName = "openai_chat"
	ProtocolOpenAIResponses ProtocolName = "openai_responses"
	ProtocolAnthropic       ProtocolName = "anthropic"
	ProtocolGemini          ProtocolName = "gemini"
	ProtocolVolcengine      ProtocolName = "volcengine"
	ProtocolOpenAIEmbedding ProtocolName = "openai_embedding"
	ProtocolUnknown         ProtocolName = "unknown"
)

type ProtocolPolicy string

const (
	ProtocolPolicyInherit          ProtocolPolicy = "inherit"
	ProtocolPolicyAuto             ProtocolPolicy = "auto"
	ProtocolPolicyPassthroughOnly  ProtocolPolicy = "passthrough-only"
	ProtocolPolicyTransformAllowed ProtocolPolicy = "transform-allowed"
)

func (p ProtocolPolicy) Normalize(defaultValue ProtocolPolicy) ProtocolPolicy {
	switch p {
	case ProtocolPolicyAuto, ProtocolPolicyPassthroughOnly, ProtocolPolicyTransformAllowed:
		return p
	case ProtocolPolicyInherit, "":
		return defaultValue
	default:
		return defaultValue
	}
}

type ProtocolFeature string

const (
	ProtocolFeatureFunctionTools        ProtocolFeature = "function_tools"
	ProtocolFeatureBuiltInTools         ProtocolFeature = "built_in_tools"
	ProtocolFeatureStructuredOutput     ProtocolFeature = "structured_output"
	ProtocolFeatureImages               ProtocolFeature = "images"
	ProtocolFeatureAudio                ProtocolFeature = "audio"
	ProtocolFeatureFiles                ProtocolFeature = "files"
	ProtocolFeatureDocuments            ProtocolFeature = "documents"
	ProtocolFeatureReasoning            ProtocolFeature = "reasoning"
	ProtocolFeatureCacheControl         ProtocolFeature = "cache_control"
	ProtocolFeatureContinuation         ProtocolFeature = "continuation"
	ProtocolFeatureResponsesState       ProtocolFeature = "responses_state"
	ProtocolFeatureProviderExtensions   ProtocolFeature = "provider_extensions"
	ProtocolFeatureAnthropicMCP         ProtocolFeature = "anthropic_mcp"
	ProtocolFeatureAnthropicContainer   ProtocolFeature = "anthropic_container"
	ProtocolFeatureAnthropicServerTools ProtocolFeature = "anthropic_server_tools"
	ProtocolFeatureGeminiExtensions     ProtocolFeature = "gemini_extensions"
	ProtocolFeatureWebSocket            ProtocolFeature = "websocket"
)

type FeatureCapability string

const (
	FeatureCapabilityNative      FeatureCapability = "native"
	FeatureCapabilityTransformed FeatureCapability = "transformed"
	FeatureCapabilityDegraded    FeatureCapability = "degraded"
	FeatureCapabilityUnsupported FeatureCapability = "unsupported"
)

type ProtocolExecutionMode string

const (
	ProtocolExecutionModePassthrough ProtocolExecutionMode = "passthrough"
	ProtocolExecutionModeTransform   ProtocolExecutionMode = "transform"
)

type ProtocolCompatibility string

const (
	ProtocolCompatibilityExact       ProtocolCompatibility = "exact"
	ProtocolCompatibilityLossy       ProtocolCompatibility = "lossy"
	ProtocolCompatibilityUnsupported ProtocolCompatibility = "unsupported"
)

type ProtocolFailureStage string

const (
	ProtocolFailureStageRoutePlanning             ProtocolFailureStage = "route_planning"
	ProtocolFailureStageRequestTransform          ProtocolFailureStage = "request_transform"
	ProtocolFailureStageOutboundStreamTransform   ProtocolFailureStage = "outbound_stream_transform"
	ProtocolFailureStageInboundStreamTransform    ProtocolFailureStage = "inbound_stream_transform"
	ProtocolFailureStageOutboundResponseTransform ProtocolFailureStage = "outbound_response_transform"
	ProtocolFailureStageInboundResponseTransform  ProtocolFailureStage = "inbound_response_transform"
)

type ProtocolRouteRequirements struct {
	InboundProtocol ProtocolName      `json:"inbound_protocol"`
	Features        []ProtocolFeature `json:"features,omitempty"`
}

type ProtocolFeatureDecision struct {
	Feature    ProtocolFeature   `json:"feature"`
	Capability FeatureCapability `json:"capability"`
	Reason     string            `json:"reason,omitempty"`
}

type ProtocolCapabilityDescriptor struct {
	InboundProtocol  ProtocolName              `json:"inbound_protocol"`
	OutboundProtocol ProtocolName              `json:"outbound_protocol"`
	Mode             ProtocolExecutionMode     `json:"mode"`
	Limited          bool                      `json:"limited,omitempty"`
	Features         []ProtocolFeatureDecision `json:"features"`
}

type RoutingStrategy string

const (
	RoutingStrategyBalanced      RoutingStrategy = "balanced"
	RoutingStrategyReliability   RoutingStrategy = "reliability"
	RoutingStrategyLowestCost    RoutingStrategy = "lowest-cost"
	RoutingStrategyLowestLatency RoutingStrategy = "lowest-latency"
	RoutingStrategyManual        RoutingStrategy = "manual"
)

func (s RoutingStrategy) Normalize() RoutingStrategy {
	switch s {
	case RoutingStrategyReliability, RoutingStrategyLowestCost, RoutingStrategyLowestLatency, RoutingStrategyManual:
		return s
	default:
		return RoutingStrategyBalanced
	}
}

type RouteCandidateStatus string

const (
	RouteCandidateActive      RouteCandidateStatus = "active"
	RouteCandidateDegraded    RouteCandidateStatus = "degraded"
	RouteCandidateStale       RouteCandidateStatus = "stale"
	RouteCandidateUnavailable RouteCandidateStatus = "unavailable"
	RouteCandidateDisabled    RouteCandidateStatus = "disabled"
	RouteCandidateArchived    RouteCandidateStatus = "archived"
)

// ModelCapability 模型能力位（来源 models.dev，只读徽标，无手动覆盖）。
// 位图存 DB（*uint8 保持三值：nil=未知、非0=能力位图），API 输出解码为 string[]。
type ModelCapability uint8

const (
	CapMultimodal  ModelCapability = 1 << iota // 多模态（image/video 输入，不含 pdf）
	CapReasoning                               // 推理（models.dev reasoning=true）
	CapVoiceInput                              // 语音输入（input 含 audio）
	CapVoiceOutput                             // 语音输出（output 含 audio）
	CapImageGen                                // 生图（output 含 image）
	CapVideoGen                                // 视频输出（output 含 video，暂不展示）
)

// CapabilityNames 位图 → API 字符串名（前端解码用，后端保持位序稳定）。
var CapabilityNames = map[ModelCapability]string{
	CapMultimodal:  "multimodal",
	CapReasoning:   "reasoning",
	CapVoiceInput:  "voice_input",
	CapVoiceOutput: "voice_output",
	CapImageGen:    "image_gen",
	CapVideoGen:    "video_gen",
}

// CapabilitiesToNames 解码位图为有序字符串数组（API 输出契约）。
func CapabilitiesToNames(caps uint8) []string {
	out := make([]string, 0, 4)
	for bit := CapMultimodal; bit <= CapVideoGen; bit <<= 1 {
		if caps&uint8(bit) != 0 {
			if name, ok := CapabilityNames[bit]; ok {
				out = append(out, name)
			}
		}
	}
	return out
}

type CanonicalModel struct {
	ID               int              `json:"id" gorm:"primaryKey"`
	Name             string           `json:"name" gorm:"size:191;not null"`
	NormalizedName   string           `json:"normalized_name" gorm:"size:191;uniqueIndex;not null"`
	Vendor           string           `json:"vendor" gorm:"size:64;index"`                 // 厂商 ID，自动识别或人工指定，空表示未知
	VendorManual     bool             `json:"vendor_manual" gorm:"not null;default:false"` // 人工指定后不再被自动识别覆盖
	VisionCapable    *bool            `json:"vision_capable,omitempty"`                    // 兼容旧字段：多模态（视觉输入），nil=未知（新数据由 Capabilities 派生）
	Capabilities     *uint8           `json:"capabilities_raw,omitempty"`                  // 能力位图（*uint8：nil=未知、非0=能力）；DB 存储 + 备份导出用（raw 数字）
	CapabilitiesList []string         `json:"capabilities,omitempty" gorm:"-"`             // API 输出解码后的能力名（如 ["multimodal","reasoning"]）；序列化层填充
	RoutingStrategy  RoutingStrategy  `json:"routing_strategy" gorm:"type:varchar(32);not null;default:'balanced'"`
	ProtocolPolicy   ProtocolPolicy   `json:"protocol_policy" gorm:"type:varchar(32);not null;default:'auto'"`
	AllowLossy       bool             `json:"allow_lossy" gorm:"not null;default:false"`
	Enabled          bool             `json:"enabled" gorm:"not null;default:true;index"`
	Manual           bool             `json:"manual" gorm:"not null;default:false"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Aliases          []ModelAlias     `json:"aliases,omitempty" gorm:"foreignKey:CanonicalModelID"`
	RouteCandidates  []RouteCandidate `json:"route_candidates,omitempty" gorm:"foreignKey:CanonicalModelID"`
}

type ModelAlias struct {
	ID               int       `json:"id" gorm:"primaryKey"`
	CanonicalModelID int       `json:"canonical_model_id" gorm:"not null;index"`
	Alias            string    `json:"alias" gorm:"size:191;not null"`
	NormalizedAlias  string    `json:"normalized_alias" gorm:"size:191;uniqueIndex;not null"`
	Manual           bool      `json:"manual" gorm:"not null;default:true"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type RouteCandidate struct {
	ID                int                  `json:"id" gorm:"primaryKey"`
	CanonicalModelID  int                  `json:"canonical_model_id" gorm:"not null;index;uniqueIndex:idx_route_candidate_identity"`
	ChannelID         int                  `json:"channel_id" gorm:"not null;index;uniqueIndex:idx_route_candidate_identity"`
	UpstreamModelName string               `json:"upstream_model_name" gorm:"size:191;not null;uniqueIndex:idx_route_candidate_identity"`
	SiteID            *int                 `json:"site_id,omitempty" gorm:"index"`
	SiteAccountID     *int                 `json:"site_account_id,omitempty" gorm:"index"`
	SiteGroupKey      string               `json:"site_group_key,omitempty" gorm:"size:128;index"`
	Status            RouteCandidateStatus `json:"status" gorm:"type:varchar(24);not null;default:'active';index"`
	Priority          int                  `json:"priority" gorm:"not null;default:0"`
	Weight            int                  `json:"weight" gorm:"not null;default:1"`
	ProtocolPolicy    ProtocolPolicy       `json:"protocol_policy" gorm:"type:varchar(32);not null;default:'inherit'"`
	AllowLossy        *bool                `json:"allow_lossy,omitempty"`
	Manual            bool                 `json:"manual" gorm:"not null;default:false"`
	LastSeenAt        time.Time            `json:"last_seen_at" gorm:"index"`
	UnavailableSince  *time.Time           `json:"unavailable_since,omitempty"`
	ArchivedAt        *time.Time           `json:"archived_at,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

type CatalogSyncResult struct {
	CanonicalCreated  int `json:"canonical_created"`
	AliasesCreated    int `json:"aliases_created"`
	CandidatesCreated int `json:"candidates_created"`
	CandidatesUpdated int `json:"candidates_updated"`
	MarkedUnavailable int `json:"marked_unavailable"`
	Archived          int `json:"archived"`
	GroupsCreated     int `json:"groups_created"`
	GroupItemsCreated int `json:"group_items_created"`
	Skipped           int `json:"skipped"` // 手动供给模式下跳过的未选中模型数
}

type RouteDecisionReason struct {
	RouteCandidateID    int                       `json:"route_candidate_id"`
	ChannelID           int                       `json:"channel_id"`
	UpstreamModel       string                    `json:"upstream_model"`
	Status              RouteCandidateStatus      `json:"status"`
	OutboundProtocol    ProtocolName              `json:"outbound_protocol"`
	ProtocolMode        ProtocolExecutionMode     `json:"protocol_mode,omitempty"`
	ProtocolPolicy      ProtocolPolicy            `json:"protocol_policy,omitempty"`
	AllowLossy          bool                      `json:"allow_lossy"`
	Compatibility       ProtocolCompatibility     `json:"compatibility,omitempty"`
	Capabilities        []ProtocolFeatureDecision `json:"capabilities,omitempty"`
	Warnings            []string                  `json:"warnings,omitempty"`
	Multiplier          *float64                  `json:"multiplier,omitempty"`
	GroupMultiplier     *float64                  `json:"group_multiplier,omitempty"`
	EffectiveMultiplier *float64                  `json:"effective_multiplier,omitempty"`
	MultiplierSource    string                    `json:"multiplier_source,omitempty"`
	MultiplierCap       *float64                  `json:"multiplier_cap,omitempty"`
	MultiplierKnown     *bool                     `json:"multiplier_known,omitempty"`
	PolicyStatus        string                    `json:"policy_status,omitempty"`
	PolicyReason        string                    `json:"policy_reason,omitempty"`
	Included            bool                      `json:"included"`
	Reason              string                    `json:"reason"`
	Score               float64                   `json:"score"`
}

type RoutePreview struct {
	RequestedModel  string                `json:"requested_model"`
	CanonicalModel  string                `json:"canonical_model"`
	InboundProtocol ProtocolName          `json:"inbound_protocol"`
	Features        []ProtocolFeature     `json:"features,omitempty"`
	Strategy        RoutingStrategy       `json:"strategy"`
	Decisions       []RouteDecisionReason `json:"decisions"`
}
