package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/tmaxmax/go-sse"
)

// ErrEmptyUpstreamStream marks 200 SSE streams that ended without forwarding
// any payload (all events skipped by transform or no events at all).
// Relay should fail over to another channel.
var ErrEmptyUpstreamStream = errors.New("upstream stream ended without forwarding any payload")

// ErrFirstTokenTimeout marks a first-token timeout. Relay detects it with
// errors.Is and converts it into a channel-switch decision.
var ErrFirstTokenTimeout = errors.New("first token timeout")

// ErrUpstreamStalled marks a mid-stream stall: the upstream stopped sending
// data entirely. Output may already be partially written, so failover is not
// possible — the stream is aborted and the attempt recorded as failed.
var ErrUpstreamStalled = errors.New("upstream stream stalled")

// StreamSource abstracts different event sources (SSE, WebSocket, raw bytes).
type StreamSource interface {
	// ReadEvent blocks until the next event is available or returns an error.
	// Returns io.EOF when the stream ends normally.
	ReadEvent(ctx context.Context) ([]byte, error)

	// Close releases resources. Must be idempotent.
	Close() error
}

// StreamTransform converts raw event data to the client's expected format.
// Returns nil/empty slice to skip writing (e.g., keep-alive events).
// For passthrough, set to nil in StreamConfig.
type StreamTransform func(ctx context.Context, data []byte) ([]byte, error)

// StreamWriter abstracts the HTTP/WebSocket response writer.
type StreamWriter interface {
	Write(data []byte) (int, error)
	Flush()
	Written() bool
	Header() http.Header
	WriteHeader(code int)
}

// StreamConfig configures a StreamProcessor instance.
type StreamConfig struct {
	// Core dependencies
	Source    StreamSource
	Transform StreamTransform // nil for passthrough
	Writer    StreamWriter
	Context   context.Context

	// Timeout & heartbeat
	FirstTokenTimeout time.Duration // 0 to disable
	HeartbeatInterval time.Duration // 0 to disable

	// InactivityTimeout 流内不活跃上限：任意上游数据都会重置计时，超过上限
	// 无数据即判停（0 禁用）。上游「200+SSE 头后静默挂死」是最常见的慢失败
	// 形态，首 token 超时只覆盖首 token，此值兜住流中途的停摆。心跳是我们
	// 发给客户端的，不重置计时——否则死上游会被自己的心跳续命。
	InactivityTimeout time.Duration

	// Callbacks
	OnFirstToken func()                                            // Called when first payload written
	OnFinish     func(ctx context.Context, rawStream []byte) error // Called on stream end

	// Passthrough-specific
	BufferRawStream bool                // Enable raw stream buffering for metrics
	TerminalEvents  map[string]struct{} // Protocol terminal events for early completion

	// MaxEventSize 是终态事件扫描时 SSE 单事件大小上限；0 表示默认 32MB。
	// 与 SSESource 的读取上限保持一致（两者由 relay 层传入同一配置值）。
	MaxEventSize int
}

// Termination describes how the transport loop ended. It is deliberately
// separate from the request outcome: a client disconnect can happen after the
// protocol has already emitted its terminal event.
type Termination string

const (
	TerminationUnknown                       Termination = "unknown"
	TerminationUpstreamEOF                   Termination = "upstream_eof"
	TerminationProtocolTerminal              Termination = "protocol_terminal"
	TerminationClientCanceled                Termination = "client_canceled"
	TerminationClientDisconnectedAfterFinish Termination = "client_disconnected_after_finish"
	TerminationFirstTokenTimeout             Termination = "first_token_timeout"
	TerminationUpstreamStalled               Termination = "upstream_stalled"
	TerminationReadError                     Termination = "read_error"
	TerminationWriteError                    Termination = "write_error"
	TerminationTransformError                Termination = "transform_error"
)

// Result is the structured completion evidence produced by StreamProcessor.
// Callers should use this instead of deriving business success from err alone.
type Result struct {
	PayloadWritten bool        `json:"payload_written"`
	TerminalEvent  string      `json:"terminal_event,omitempty"`
	Termination    Termination `json:"termination"`
}

// StreamProcessor unifies all stream handling logic.
type StreamProcessor struct {
	config StreamConfig

	// State
	rawBuffer      bytes.Buffer
	payloadWritten bool
	firstToken     bool
	terminalEvent  string
	termination    Termination
}

// NewStreamProcessor creates a processor from config.
func NewStreamProcessor(config StreamConfig) *StreamProcessor {
	return &StreamProcessor{
		config:      config,
		firstToken:  true,
		termination: TerminationUnknown,
	}
}

// Run executes the unified stream processing loop.
func (p *StreamProcessor) Run() error {
	// Set SSE response headers
	headers := p.config.Writer.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")

	// Setup heartbeat ticker
	var heartbeatTicker *time.Ticker
	var heartbeatC <-chan time.Time
	if p.config.HeartbeatInterval > 0 {
		heartbeatTicker = time.NewTicker(p.config.HeartbeatInterval)
		heartbeatC = heartbeatTicker.C
		defer heartbeatTicker.Stop()
	}

	// Setup first token timeout
	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if p.firstToken && p.config.FirstTokenTimeout > 0 {
		firstTokenTimer = time.NewTimer(p.config.FirstTokenTimeout)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	// Setup inactivity timeout (mid-stream stall detection)
	var inactivityTimer *time.Timer
	var inactivityC <-chan time.Time
	if p.config.InactivityTimeout > 0 {
		inactivityTimer = time.NewTimer(p.config.InactivityTimeout)
		inactivityC = inactivityTimer.C
		defer inactivityTimer.Stop()
	}

	// Async read from source — use a derived context so we can unblock on any exit.
	readCtx, readCancel := context.WithCancel(p.config.Context)
	// 退出顺序（defer LIFO）：先取消读 ctx，再 Close。WS Source 的 Close 会把
	// 连接归还连接池，必须先取消让在途的 conn.Read(ctx) 尽快醒来；取消传播
	// 窗口内的连接安全由 wsUpstreamReader.Close 的在途读标记兜底（弃用不回池）。
	defer p.config.Source.Close()
	defer readCancel()

	type readResult struct {
		data []byte
		err  error
	}
	results := make(chan readResult, 1)
	safe.Go("stream-processor-read", func() {
		defer close(results)
		for {
			data, err := p.config.Source.ReadEvent(readCtx)
			select {
			case results <- readResult{data: data, err: err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	})

	// Main event loop
	for {
		select {
		case <-p.config.Context.Done():
			return p.handleDisconnect()

		case <-firstTokenC:
			p.termination = TerminationFirstTokenTimeout
			return p.handleFirstTokenTimeout()

		case <-inactivityC:
			p.termination = TerminationUpstreamStalled
			return p.handleUpstreamStalled()

		case <-heartbeatC:
			if err := p.writeHeartbeat(); err != nil {
				return err
			}

		case r, ok := <-results:
			if inactivityTimer != nil {
				// 任何上游事件（含错误事件）都是活动；用 drain+Reset 防丢信号
				if !inactivityTimer.Stop() {
					select {
					case <-inactivityC:
					default:
					}
				}
				inactivityTimer.Reset(p.config.InactivityTimeout)
			}
			if !ok {
				// The reader can exit through readCtx.Done without publishing its
				// final context error. Do not reinterpret that close as upstream EOF.
				if p.config.Context.Err() != nil {
					return p.handleDisconnect()
				}
				p.termination = p.successfulTermination()
				return p.finalize()
			}

			if r.err != nil {
				if r.err == io.EOF {
					p.termination = p.successfulTermination()
					return p.finalize()
				}
				if p.config.Context.Err() != nil &&
					(errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded)) {
					return p.handleDisconnect()
				}
				p.termination = TerminationReadError
				err := fmt.Errorf("stream read error: %w", r.err)
				// F17：读失败前已收到的部分流仍可能携带计费信息（如 Anthropic
				// message_start 的 input usage），跳过 OnFinish 会丢账。与断连
				// 路径一致回调，幂等由 OnFinish 实现方保证。
				if p.config.BufferRawStream && p.rawBuffer.Len() > 0 && p.config.OnFinish != nil {
					_ = p.config.OnFinish(context.Background(), p.rawBuffer.Bytes())
				}
				return err
			}

			if len(r.data) == 0 {
				continue
			}

			// Buffer raw data if enabled
			if p.config.BufferRawStream {
				p.rawBuffer.Write(r.data)
			}

			// Transform and write
			if err := p.processEvent(r.data); err != nil {
				return err
			}

			// First token handling
			if p.firstToken && p.payloadWritten {
				p.firstToken = false
				if p.config.OnFirstToken != nil {
					p.config.OnFirstToken()
				}
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}
		}
	}
}

// processEvent transforms and writes a single event.
func (p *StreamProcessor) processEvent(data []byte) error {
	var output []byte
	var err error

	if p.config.Transform != nil {
		output, err = p.config.Transform(p.config.Context, data)
		if err != nil {
			p.termination = TerminationTransformError
			return fmt.Errorf("transform error: %w", err)
		}
		if len(output) == 0 {
			return nil // Skip empty output
		}
	} else {
		output = data // Passthrough
	}

	// F16：上游 SSE 心跳（纯注释块）原样透传但不计为首 token/有效载荷——
	// TTFT 的语义是「客户端看到首个真实内容」。心跳+真实数据混合的块仍按
	// 有效载荷计，按字节切分的 chunk 边界不会误判。
	commentOnlyPassthrough := p.config.Transform == nil && isSSECommentOnly(output)

	// Record protocol terminal evidence before writing. A downstream write can
	// fail immediately after the upstream emitted its terminal frame; losing
	// the evidence would incorrectly turn a completed response into a generic
	// transport failure.
	if p.terminalEvent == "" && outputMayContainTerminalEvent(output, p.config.TerminalEvents) {
		p.terminalEvent = terminalEventFromSSE(output, p.config.TerminalEvents, p.config.MaxEventSize)
	}
	if _, err := p.config.Writer.Write(output); err != nil {
		p.termination = TerminationWriteError
		if p.rawBuffer.Len() > 0 && p.config.OnFinish != nil && p.config.BufferRawStream {
			// The normal finalize path is skipped on a write error, but the
			// already received stream (terminal frame or partial usage frames)
			// still carries accounting metadata.
			_ = p.config.OnFinish(context.Background(), p.rawBuffer.Bytes())
		}
		return fmt.Errorf("write error: %w", err)
	}

	if !commentOnlyPassthrough {
		p.payloadWritten = true
	}
	p.config.Writer.Flush()
	return nil
}

// isSSECommentOnly 判断一段透传字节是否全部由 SSE 注释行/空行组成
// （注释行以冒号开头）。data 行哪怕只有一行也返回 false。
func isSSECommentOnly(payload []byte) bool {
	sawAny := false
	for len(payload) > 0 {
		line := payload
		if idx := bytes.IndexByte(payload, '\n'); idx >= 0 {
			line, payload = payload[:idx], payload[idx+1:]
		} else {
			payload = nil
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		sawAny = true
		if trimmed[0] != ':' {
			return false
		}
	}
	return sawAny
}

// writeHeartbeat sends SSE heartbeat (comment line).
func (p *StreamProcessor) writeHeartbeat() error {
	if _, err := p.config.Writer.Write([]byte(":\n\n")); err != nil {
		return err
	}
	p.config.Writer.Flush()
	return nil
}

// handleDisconnect handles context cancellation or timeout.
func (p *StreamProcessor) handleDisconnect() error {
	// Check both transformed output (recorded during processEvent) and buffered
	// source bytes. The former is essential for Chat/Anthropic -> Responses
	// conversions where only the client-facing stream contains response.completed.
	if p.terminalEvent == "" && p.config.BufferRawStream && len(p.config.TerminalEvents) > 0 {
		p.terminalEvent = p.streamTerminalEvent()
	}
	if p.terminalEvent != "" {
		p.termination = TerminationClientDisconnectedAfterFinish
		log.Debugf("client disconnected after terminal event %s, treating as success", p.terminalEvent)
		return p.finalizeWithContext(context.Background())
	}

	err := p.config.Context.Err()
	p.termination = TerminationClientCanceled
	log.Debugf("client disconnected, stopping stream: written=%t first_token_seen=%t err=%v",
		p.payloadWritten, !p.firstToken, err)

	if p.config.BufferRawStream && p.rawBuffer.Len() > 0 {
		// Still call OnFinish to collect partial metrics
		if p.config.OnFinish != nil {
			_ = p.config.OnFinish(context.Background(), p.rawBuffer.Bytes())
		}
	}

	return err
}

// handleUpstreamStalled returns mid-stream stall error.
func (p *StreamProcessor) handleUpstreamStalled() error {
	return fmt.Errorf("no upstream data for %v: %w", p.config.InactivityTimeout, ErrUpstreamStalled)
}

// handleFirstTokenTimeout returns first token timeout error.
func (p *StreamProcessor) handleFirstTokenTimeout() error {
	log.Warnf("first token timeout (%v), switching channel", p.config.FirstTokenTimeout)
	return fmt.Errorf("first token timeout after %v: %w", p.config.FirstTokenTimeout, ErrFirstTokenTimeout)
}

// finalize completes the stream and calls OnFinish callback.
func (p *StreamProcessor) finalize() error {
	return p.finalizeWithContext(p.config.Context)
}

func (p *StreamProcessor) finalizeWithContext(ctx context.Context) error {
	if !p.payloadWritten {
		return ErrEmptyUpstreamStream
	}

	log.Debugf("stream end (payload_written=%t)", p.payloadWritten)

	if p.config.OnFinish != nil {
		rawStream := p.rawBuffer.Bytes()
		if err := p.config.OnFinish(ctx, rawStream); err != nil {
			return err
		}
	}

	return nil
}

// PayloadWritten returns whether any payload has been written to the client.
func (p *StreamProcessor) PayloadWritten() bool {
	return p.payloadWritten
}

// Result returns the completion evidence accumulated by the processor.
func (p *StreamProcessor) Result() Result {
	return Result{
		PayloadWritten: p.payloadWritten,
		TerminalEvent:  p.terminalEvent,
		Termination:    p.termination,
	}
}

func (p *StreamProcessor) successfulTermination() Termination {
	if p.terminalEvent == "" && p.config.BufferRawStream {
		p.terminalEvent = p.streamTerminalEvent()
	}
	if p.terminalEvent != "" {
		return TerminationProtocolTerminal
	}
	return TerminationUpstreamEOF
}

// streamTerminalEvent checks if buffered stream contains a terminal event.
func (p *StreamProcessor) streamTerminalEvent() string {
	if p.rawBuffer.Len() == 0 {
		return ""
	}
	return terminalEventFromSSE(p.rawBuffer.Bytes(), p.config.TerminalEvents, p.config.MaxEventSize)
}

// outputMayContainTerminalEvent 廉价子串预筛（C250913-14）：每个 chunk 完整
// 重跑 SSE 解析 + JSON 扫描此前是无条件执行的，而绝大多数 chunk 不含终态帧。
// 先对候选终态名做 bytes.Contains，未命中直接跳过解析（约省 5-15% 单核）。
func outputMayContainTerminalEvent(payload []byte, terminalEvents map[string]struct{}) bool {
	if len(payload) == 0 || len(terminalEvents) == 0 {
		return false
	}
	for name := range terminalEvents {
		if bytes.Contains(payload, []byte(name)) {
			return true
		}
	}
	return false
}

func terminalEventFromSSE(payload []byte, terminalEvents map[string]struct{}, maxEventSize int) string {
	if len(payload) == 0 || len(terminalEvents) == 0 {
		return ""
	}
	if maxEventSize <= 0 {
		maxEventSize = 32 * 1024 * 1024 // 32MB default
	}
	readCfg := &sse.ReadConfig{MaxEventSize: maxEventSize}
	for ev, err := range sse.Read(bytes.NewReader(payload), readCfg) {
		if err != nil {
			break
		}

		// Extract event type
		typ := strings.TrimSpace(ev.Type)
		if typ == "" {
			data := strings.TrimSpace(ev.Data)
			if data == "[DONE]" {
				typ = "[DONE]"
			}
			if len(data) > 0 && data[0] == '{' {
				typ = extractJSONType(data)
			}
		}

		if _, ok := terminalEvents[typ]; ok {
			return typ
		}
	}

	return ""
}

// extractJSONType extracts "type" field from JSON without full unmarshaling.
func extractJSONType(data string) string {
	// Simple extraction: find "type":"value"
	if idx := strings.Index(data, `"type"`); idx >= 0 {
		rest := data[idx+6:]
		if idx := strings.IndexByte(rest, ':'); idx >= 0 {
			rest = rest[idx+1:]
			rest = strings.TrimSpace(rest)
			if len(rest) > 0 && rest[0] == '"' {
				rest = rest[1:]
				if idx := strings.IndexByte(rest, '"'); idx >= 0 {
					return rest[:idx]
				}
			}
		}
	}
	return ""
}
