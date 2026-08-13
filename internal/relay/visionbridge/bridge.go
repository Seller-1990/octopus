package visionbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// Action 是 bridge 对单个候选通道模型的处置决定。
type Action int

const (
	// ActionPassthrough 原样转发：视觉通道或能力未知（保守不误替换）。
	ActionPassthrough Action = iota
	// ActionBridge 已知纯文本通道：图片必须替换为描述，替换失败则跳过该通道（绝不原图透传）。
	ActionBridge
)

// service 持有跨请求单例：描述缓存与 VLM 并发信号量（尺寸为编译期常量，与运行时设置解耦）。
type service struct {
	cache   *analysisCache
	limiter *inflightLimiter
}

var (
	svcOnce sync.Once
	svc     *service
)

func getService() *service {
	svcOnce.Do(func() {
		svc = &service{
			cache:   newAnalysisCache(cacheSize),
			limiter: newInflightLimiter(maxInflight),
		}
	})
	return svc
}

// ResetServiceForTest 重建单例缓存与信号量——仅测试隔离用：analysis cache 跨请求
// 存活（TTL 900s）且 cacheKey 无 per-run 熵，-count>1 重复跑会命中上一轮缓存，
// 破坏「VLM 恰好调用 N 次」类断言。
func ResetServiceForTest() {
	svcOnce = sync.Once{}
	svc = nil
}

// Active 报告 bridge 全局配置是否生效（总开关 + 必填项齐备）。
// compact 等 raw 直通入口用它决定是否需要对纯文本通道做保护性跳过。
func (c Config) Active() bool {
	return c.Enabled && c.VisionModel != "" && c.VisionBaseURL != ""
}

// RequestState 承载单个 relay 请求的 bridge 生命周期：
// 配置快照 + 图片发现结果 + 跨通道 memoize 的 VLM 描述与重写请求。
type RequestState struct {
	svc             *service
	cfg             Config
	origReq         *model.InternalLLMRequest
	refs            []ImageRef
	discoverErr     error
	focusHint       string
	canonicalName   string
	canonicalVision *bool

	mu       sync.Mutex
	done     bool
	prepared *model.InternalLLMRequest
	prepErr  error
	// capMemo 缓存每个上游模型名的能力判定：一次请求内 PreferChannel（分区）与
	// ActionFor（换入判定）必须看到同一结论——modelvendor 索引可被同步任务热替换，
	// 两次读取间不一致会让「分区判纯文本、换入判未知」把原图直通给纯文本模型。
	capMemo map[string]visionCap
}

type visionCap struct {
	vision bool
	known  bool
}

// NewRequestState 在「key 级开关 ∧ 全局设置生效 ∧ 请求含图」时返回状态对象，
// 否则返回 nil（零开销路径）。canonicalName/canonicalVision 是请求解析出的
// CanonicalModel 名称与能力位（nil=未解析/未知），仅当上游模型与 canonical
// 同名时才作为能力查询未命中的兜底证据——不同名映射的能力不可传染。
func NewRequestState(req *model.InternalLLMRequest, canonicalName string, canonicalVision *bool, keyOptIn bool) *RequestState {
	if !keyOptIn || req == nil || !req.HasImages() {
		return nil
	}
	cfg := snapshotSettings()
	if !cfg.Active() {
		return nil
	}
	refs, err := Discover(req, cfg)
	if err == nil && len(refs) == 0 {
		return nil
	}
	return &RequestState{
		svc:             getService(),
		cfg:             cfg,
		origReq:         req,
		refs:            refs,
		discoverErr:     err,
		focusHint:       lastUserText(req),
		canonicalName:   strings.TrimSpace(canonicalName),
		canonicalVision: canonicalVision,
		capMemo:         make(map[string]visionCap),
	}
}

// ActionFor 决定候选通道模型的处置：仅当模型被证实无视觉能力才替换。
func (st *RequestState) ActionFor(upstreamModel string) Action {
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
	st.mu.Lock()
	defer st.mu.Unlock()
	if cached, ok := st.capMemo[upstreamModel]; ok {
		return cached.vision, cached.known
	}
	vision, known = modelvendor.LookupVision(upstreamModel)
	if !known && st.canonicalVision != nil &&
		strings.EqualFold(strings.TrimSpace(upstreamModel), st.canonicalName) {
		// canonical 能力位只对同名模型作证据：item.ModelName 是通道上游模型，
		// 可与客户端请求的 canonical 模型毫无关系（管理员自由映射）。
		vision, known = *st.canonicalVision, true
	}
	st.capMemo[upstreamModel] = visionCap{vision: vision, known: known}
	return vision, known
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
	prompt := BuildPrompt("auto", st.focusHint, len(st.refs))
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
	text, err := analyzeChain(ctx, st.cfg, st.refs, prompt)
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
// 每段先写长度前缀：prompt 含用户可控文本，禁止靠分隔符划界（注入分隔符可制造边界歧义）。
func (st *RequestState) cacheKey(prompt string) string {
	h := sha256.New()
	for _, field := range append(
		[]string{promptVersion, prompt, strings.Join(modelChain(st.cfg), ",")},
		identities(st.refs)...,
	) {
		fmt.Fprintf(h, "%d:", len(field))
		io.WriteString(h, field)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// cacheTTL：任一图片是 URL 引用（内容可变）用短 TTL，纯 data URI（内容寻址）用长 TTL。
func (st *RequestState) cacheTTL() time.Duration {
	for _, ref := range st.refs {
		if !ref.IsDataURI {
			return st.cfg.URLCacheTTL
		}
	}
	return st.cfg.CacheTTL
}

// identities 惰性计算图片缓存身份（data URI 全量 SHA256 有感知成本，
// 只在真正要查/写缓存的 prepare 路径上算，纯透传请求不付这笔开销）。
func identities(refs []ImageRef) []string {
	out := make([]string, len(refs))
	for i, ref := range refs {
		out[i] = imageIdentity(ref.URL)
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
