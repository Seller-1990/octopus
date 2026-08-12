package visionbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"sync"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// Action 是 bridge 对单个候选通道的处置决定。
type Action int

const (
	// ActionPassthrough 原样转发：视觉通道、能力未知（保守不误替换）、或通道不在 bridge 白名单。
	ActionPassthrough Action = iota
	// ActionBridge 已知纯文本通道：图片必须替换为描述，替换失败则跳过该通道（绝不原图透传）。
	ActionBridge
)

type service struct {
	cfg     Config
	cache   *analysisCache
	limiter *inflightLimiter
	client  *vlmClient
}

var (
	svcOnce sync.Once
	svc     *service
)

func newService(cfg Config) *service {
	return &service{
		cfg:     cfg,
		cache:   newAnalysisCache(cfg.CacheSize),
		limiter: newInflightLimiter(cfg.MaxInflight),
		client:  newVLMClient(cfg),
	}
}

func getService() *service {
	svcOnce.Do(func() {
		s := newService(loadConfig())
		if s.cfg.Enabled && !s.active() {
			log.Warnf("vision bridge enabled but vision_model/vision_base_url not configured; bridge stays inactive")
		}
		svc = s
	})
	return svc
}

func (s *service) active() bool {
	return s.cfg.Enabled && strings.TrimSpace(s.cfg.VisionModel) != "" &&
		strings.TrimSpace(s.cfg.VisionBaseURL) != ""
}

// RequestState 承载单个 relay 请求的 bridge 生命周期：
// 图片发现结果 + 跨通道 memoize 的 VLM 描述与重写请求。
type RequestState struct {
	svc             *service
	origReq         *model.InternalLLMRequest
	refs            []ImageRef
	discoverErr     error
	focusHint       string
	canonicalVision *bool

	mu       sync.Mutex
	done     bool
	prepared *model.InternalLLMRequest
	prepErr  error
}

// NewRequestState 在请求含图且 bridge 生效时返回状态对象，否则返回 nil（零开销路径）。
// canonicalVision 是请求解析出的 CanonicalModel.VisionCapable（nil=未解析/未知），
// 作为上游模型名能力查询未命中时的兜底证据。
func NewRequestState(req *model.InternalLLMRequest, canonicalVision *bool) *RequestState {
	s := getService()
	if !s.active() || req == nil || !req.HasImages() {
		return nil
	}
	refs, err := Discover(req, s.cfg)
	if err == nil && len(refs) == 0 {
		return nil
	}
	return &RequestState{
		svc:             s,
		origReq:         req,
		refs:            refs,
		discoverErr:     err,
		focusHint:       lastUserText(req),
		canonicalVision: canonicalVision,
	}
}

// ActionFor 决定候选通道的处置：仅当模型被证实无视觉能力且通道在 bridge 范围内才替换。
func (st *RequestState) ActionFor(channelID int, upstreamModel string) Action {
	if st.svc.cfg.TargetChannelIDs != nil {
		if _, ok := st.svc.cfg.TargetChannelIDs[channelID]; !ok {
			return ActionPassthrough
		}
	}
	if vision, known := st.visionCapability(upstreamModel); known && !vision {
		return ActionBridge
	}
	return ActionPassthrough
}

// PreferChannel 是含图请求的通道排序谓词：视觉可用或能力未知的通道优先
// （零额外延迟透传），已知纯文本通道退居兜底再走 bridge。
func (st *RequestState) PreferChannel(item dbmodel.GroupItem) bool {
	vision, known := st.visionCapability(item.ModelName)
	return !known || vision
}

func (st *RequestState) visionCapability(upstreamModel string) (vision, known bool) {
	if vision, known = modelvendor.LookupVision(upstreamModel); known {
		return vision, true
	}
	if st.canonicalVision != nil {
		return *st.canonicalVision, true
	}
	return false, false
}

// BridgedRequest 返回图片替换为 VLM 描述后的请求副本；VLM 调用与重写结果
// 跨通道 memoize（含失败：VLM 链整体失败后其余纯文本通道快速跳过）。
func (st *RequestState) BridgedRequest(ctx context.Context) (*model.InternalLLMRequest, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.done {
		return st.prepared, st.prepErr
	}
	st.done = true
	st.prepared, st.prepErr = st.prepare(ctx)
	return st.prepared, st.prepErr
}

func (st *RequestState) prepare(ctx context.Context) (*model.InternalLLMRequest, error) {
	if st.discoverErr != nil {
		return nil, st.discoverErr
	}
	prompt := BuildPrompt(st.svc.cfg.Language, st.focusHint, len(st.refs))
	key := st.cacheKey(prompt)
	if text, ok := st.svc.cache.Get(key); ok {
		log.Debugf("vision bridge: analysis cache hit (%d images)", len(st.refs))
		return RewriteRequest(st.origReq, st.refs, text)
	}
	if err := st.svc.limiter.Acquire(ctx); err != nil {
		return nil, err
	}
	defer st.svc.limiter.Release()

	start := time.Now()
	text, err := st.svc.client.Analyze(ctx, st.refs, prompt)
	if err != nil {
		return nil, err
	}
	st.svc.cache.Set(key, text, st.cacheTTL())
	log.Infow("vision bridge: analysis complete",
		"images", len(st.refs),
		"chars", len([]rune(text)),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return RewriteRequest(st.origReq, st.refs, text)
}

// cacheKey = SHA256(prompt 版本 + 完整提示词 + VLM 模型链 + 全部图片身份)。
func (st *RequestState) cacheKey(prompt string) string {
	h := sha256.New()
	for _, field := range append(
		[]string{promptVersion, prompt, strings.Join(st.svc.client.modelChain(), ",")},
		identities(st.refs)...,
	) {
		io.WriteString(h, field)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// cacheTTL：任一图片是 URL 引用（内容可变）用短 TTL，纯 data URI（内容寻址）用长 TTL。
func (st *RequestState) cacheTTL() time.Duration {
	for _, ref := range st.refs {
		if !ref.IsDataURI {
			return st.svc.cfg.URLCacheTTL
		}
	}
	return st.svc.cfg.CacheTTL
}

func identities(refs []ImageRef) []string {
	out := make([]string, len(refs))
	for i, ref := range refs {
		out[i] = ref.Identity
	}
	return out
}

// lastUserText 提取最后一条 user 消息的文本内容作为 VLM focus hint。
func lastUserText(req *model.InternalLLMRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		msg := &req.Messages[i]
		if msg.Role != "user" {
			continue
		}
		if msg.Content.Content != nil {
			return *msg.Content.Content
		}
		var texts []string
		for j := range msg.Content.MultipleContent {
			part := &msg.Content.MultipleContent[j]
			if part.Type == "text" && part.Text != nil {
				texts = append(texts, *part.Text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	return ""
}
