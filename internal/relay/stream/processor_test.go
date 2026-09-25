package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockStreamWriter implements StreamWriter for testing.
type mockStreamWriter struct {
	buffer  bytes.Buffer
	written bool
	headers http.Header
}

type failingStreamWriter struct {
	headers http.Header
}

func (f *failingStreamWriter) Write([]byte) (int, error) {
	return 0, errors.New("downstream write failed")
}
func (f *failingStreamWriter) Flush()              {}
func (f *failingStreamWriter) Written() bool       { return false }
func (f *failingStreamWriter) Header() http.Header { return f.headers }
func (f *failingStreamWriter) WriteHeader(int)     {}

func newMockStreamWriter() *mockStreamWriter {
	return &mockStreamWriter{
		headers: make(http.Header),
	}
}

func (m *mockStreamWriter) Write(data []byte) (int, error) {
	m.written = true
	return m.buffer.Write(data)
}

func (m *mockStreamWriter) Flush() {}

func (m *mockStreamWriter) Written() bool {
	return m.written
}

func (m *mockStreamWriter) Header() http.Header {
	return m.headers
}

func (m *mockStreamWriter) WriteHeader(code int) {}

// mockStreamSource implements StreamSource for testing.
type mockStreamSource struct {
	events [][]byte
	index  int
	closed bool
}

func newMockStreamSource(events [][]byte) *mockStreamSource {
	return &mockStreamSource{events: events}
}

func (m *mockStreamSource) ReadEvent(ctx context.Context) ([]byte, error) {
	if m.index >= len(m.events) {
		return nil, io.EOF
	}
	data := m.events[m.index]
	m.index++
	return data, nil
}

func (m *mockStreamSource) Close() error {
	m.closed = true
	return nil
}

func TestStreamProcessor_BasicFlow(t *testing.T) {
	events := [][]byte{
		[]byte(`{"data":"chunk1"}`),
		[]byte(`{"data":"chunk2"}`),
		[]byte(`{"data":"chunk3"}`),
	}

	source := newMockStreamSource(events)
	writer := newMockStreamWriter()
	ctx := context.Background()

	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: ctx,
	})

	err := processor.Run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !source.closed {
		t.Error("source not closed")
	}

	output := writer.buffer.String()
	for _, event := range events {
		if !strings.Contains(output, string(event)) {
			t.Errorf("output missing event: %s", event)
		}
	}
}

func TestStreamProcessor_WithTransform(t *testing.T) {
	events := [][]byte{
		[]byte(`chunk1`),
		[]byte(`chunk2`),
	}

	source := newMockStreamSource(events)
	writer := newMockStreamWriter()
	ctx := context.Background()

	transform := func(ctx context.Context, data []byte) ([]byte, error) {
		// Wrap in SSE format
		return []byte("data: " + string(data) + "\n\n"), nil
	}

	processor := NewStreamProcessor(StreamConfig{
		Source:    source,
		Writer:    writer,
		Context:   ctx,
		Transform: transform,
	})

	err := processor.Run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := writer.buffer.String()
	expected := "data: chunk1\n\ndata: chunk2\n\n"
	if output != expected {
		t.Errorf("unexpected output:\ngot:  %q\nwant: %q", output, expected)
	}
}

func TestStreamProcessor_FirstTokenCallback(t *testing.T) {
	events := [][]byte{
		[]byte(`chunk1`),
		[]byte(`chunk2`),
	}

	source := newMockStreamSource(events)
	writer := newMockStreamWriter()
	ctx := context.Background()

	firstTokenCalled := false
	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: ctx,
		OnFirstToken: func() {
			firstTokenCalled = true
		},
	})

	err := processor.Run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !firstTokenCalled {
		t.Error("OnFirstToken not called")
	}
}

func TestStreamProcessor_EmptyStream(t *testing.T) {
	source := newMockStreamSource([][]byte{})
	writer := newMockStreamWriter()
	ctx := context.Background()

	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: ctx,
	})

	err := processor.Run()
	if err == nil {
		t.Fatal("expected error for empty stream")
	}

	if !errors.Is(err, ErrEmptyUpstreamStream) {
		t.Errorf("expected ErrEmptyUpstreamStream, got: %v", err)
	}
}

func TestStreamProcessor_ContextCancellation(t *testing.T) {
	// Source that emits one chunk then blocks until cancelled
	source := &cancelTestSource{first: []byte(`chunk1`)}
	writer := newMockStreamWriter()
	ctx, cancel := context.WithCancel(context.Background())

	firstTokenSeen := make(chan struct{})
	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: ctx,
		OnFirstToken: func() {
			close(firstTokenSeen)
		},
	})

	errChan := make(chan error, 1)
	go func() {
		errChan <- processor.Run()
	}()

	// Wait until first token is processed, then cancel
	<-firstTokenSeen
	cancel()

	err := <-errChan
	if err == nil {
		t.Fatal("expected context cancellation error")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// cancelTestSource emits one chunk then blocks until context is cancelled.
type cancelTestSource struct {
	first  []byte
	sent   bool
	closed bool
}

func (s *cancelTestSource) ReadEvent(ctx context.Context) ([]byte, error) {
	if !s.sent {
		s.sent = true
		return s.first, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *cancelTestSource) Close() error {
	s.closed = true
	return nil
}

func TestStreamProcessor_BufferRawStream(t *testing.T) {
	events := [][]byte{
		[]byte(`chunk1`),
		[]byte(`chunk2`),
	}

	source := newMockStreamSource(events)
	writer := newMockStreamWriter()
	ctx := context.Background()

	var bufferedData []byte
	processor := NewStreamProcessor(StreamConfig{
		Source:          source,
		Writer:          writer,
		Context:         ctx,
		BufferRawStream: true,
		OnFinish: func(ctx context.Context, rawStream []byte) error {
			bufferedData = rawStream
			return nil
		},
	})

	err := processor.Run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "chunk1chunk2"
	if string(bufferedData) != expected {
		t.Errorf("unexpected buffered data:\ngot:  %q\nwant: %q", bufferedData, expected)
	}
}

func TestStreamProcessor_FirstTokenTimeout(t *testing.T) {
	// Source that blocks forever
	blockingSource := &blockingStreamSource{}
	writer := newMockStreamWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	processor := NewStreamProcessor(StreamConfig{
		Source:            blockingSource,
		Writer:            writer,
		Context:           ctx,
		FirstTokenTimeout: 10 * time.Millisecond,
	})

	err := processor.Run()
	if err == nil {
		t.Fatal("expected timeout error")
	}

	if !strings.Contains(err.Error(), "first token timeout") {
		t.Errorf("unexpected error: %v", err)
	}
}

// blockingStreamSource blocks forever in ReadEvent.
type blockingStreamSource struct {
	closed bool
}

func (s *blockingStreamSource) ReadEvent(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *blockingStreamSource) Close() error {
	s.closed = true
	return nil
}

func TestStreamProcessor_TerminalEventDetection(t *testing.T) {
	// SSE stream with terminal event
	sseData := `data: {"type":"message_start"}

data: {"type":"content_block_delta"}

data: {"type":"message_stop"}

`
	source := newMockStreamSource([][]byte{[]byte(sseData)})
	writer := newMockStreamWriter()
	ctx, cancel := context.WithCancel(context.Background())

	terminalEvents := map[string]struct{}{
		"message_stop": {},
	}

	firstTokenSeen := make(chan struct{})
	processor := NewStreamProcessor(StreamConfig{
		Source:          source,
		Writer:          writer,
		Context:         ctx,
		BufferRawStream: true,
		TerminalEvents:  terminalEvents,
		OnFirstToken: func() {
			close(firstTokenSeen)
		},
	})

	// Start processor in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- processor.Run()
	}()

	// Wait until first token is written, then cancel
	<-firstTokenSeen
	cancel()

	// Should treat as success because terminal event was reached
	err := <-errChan
	if err != nil {
		t.Errorf("expected nil error due to terminal event detection, got: %v", err)
	}
}

func TestStreamProcessorKeepsTerminalEvidenceWhenWriteFails(t *testing.T) {
	source := newMockStreamSource([][]byte{[]byte(
		"data: {\"type\":\"response.completed\"}\n\n",
	)})
	writer := &failingStreamWriter{headers: make(http.Header)}
	finished := false
	processor := NewStreamProcessor(StreamConfig{
		Source:          source,
		Writer:          writer,
		Context:         context.Background(),
		BufferRawStream: true,
		TerminalEvents:  map[string]struct{}{"response.completed": {}},
		OnFinish: func(context.Context, []byte) error {
			finished = true
			return nil
		},
	})

	if err := processor.Run(); err == nil {
		t.Fatal("expected downstream write error")
	}
	result := processor.Result()
	if result.TerminalEvent != "response.completed" ||
		result.Termination != TerminationWriteError {
		t.Fatalf("terminal evidence was lost: %+v", result)
	}
	if !finished {
		t.Fatal("terminal write failure skipped metrics finalization")
	}
}

func TestRawSource(t *testing.T) {
	data := []byte("raw chunk data")
	reader := io.NopCloser(bytes.NewReader(data))

	source := NewRawSource(reader, 10)
	defer source.Close()

	var chunks [][]byte
	for {
		chunk, err := source.ReadEvent(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}
	}

	// Reconstruct data
	result := bytes.Join(chunks, nil)
	if !bytes.Equal(result, data) {
		t.Errorf("data mismatch:\ngot:  %q\nwant: %q", result, data)
	}
}

func TestSSESource(t *testing.T) {
	sseData := `data: event1

data: event2

data: event3

`
	reader := io.NopCloser(strings.NewReader(sseData))
	source := NewSSESource(reader, 0)
	defer source.Close()

	expected := []string{"event1", "event2", "event3"}
	var events []string

	for {
		data, err := source.ReadEvent(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, string(data))
	}

	if len(events) != len(expected) {
		t.Fatalf("event count mismatch: got %d, want %d", len(events), len(expected))
	}

	for i, ev := range events {
		if ev != expected[i] {
			t.Errorf("event[%d] mismatch: got %q, want %q", i, ev, expected[i])
		}
	}
}

// TestStreamProcessor_InactivityTimeout C250913-02 回归：上游 200+SSE 头后
// 静默挂死时，流内不活跃上限必须在预算内判停，而不是让请求无界堆积。
func TestStreamProcessor_InactivityTimeout(t *testing.T) {
	source := newMockStreamSource([][]byte{[]byte(`{"data":"chunk1"}`)})
	// mock 读完即 EOF——为模拟「挂死」，包一层永不返回的 source
	hanging := &hangingSource{sent: source}
	writer := newMockStreamWriter()

	processor := NewStreamProcessor(StreamConfig{
		Source:            hanging,
		Writer:            writer,
		Context:           context.Background(),
		InactivityTimeout: 50 * time.Millisecond,
	})

	start := time.Now()
	err := processor.Run()
	elapsed := time.Since(start)
	if !errors.Is(err, ErrUpstreamStalled) {
		t.Fatalf("expected ErrUpstreamStalled, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("stall detection took too long: %v", elapsed)
	}
	if processor.Result().Termination != TerminationUpstreamStalled {
		t.Fatalf("unexpected termination: %v", processor.Result().Termination)
	}
	if !hanging.closed {
		t.Error("source not closed")
	}
}

// hangingSource 先转发预置事件，之后永远阻塞直到 ctx 取消。
type hangingSource struct {
	sent          StreamSource
	sentExhausted bool
	closed        bool
}

func (h *hangingSource) ReadEvent(ctx context.Context) ([]byte, error) {
	if !h.sentExhausted {
		data, err := h.sent.ReadEvent(ctx)
		if err == io.EOF {
			h.sentExhausted = true
		} else {
			return data, err
		}
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (h *hangingSource) Close() error {
	h.closed = true
	return nil
}

// errorAfterDataStreamSource 先投递若干事件，然后读失败（非 EOF）。
type errorAfterDataStreamSource struct {
	events [][]byte
	err    error
	idx    int
}

func (s *errorAfterDataStreamSource) ReadEvent(context.Context) ([]byte, error) {
	if s.idx < len(s.events) {
		data := s.events[s.idx]
		s.idx++
		return data, nil
	}
	return nil, s.err
}

func (s *errorAfterDataStreamSource) Close() error { return nil }

// F17：读失败前已收到的部分流必须回调 OnFinish——Anthropic 等协议在
// message_start 就携带 input usage，跳过回调会把已收到的计费信息丢账。
func TestStreamProcessor_ReadErrorStillFinishesPartialStream(t *testing.T) {
	source := &errorAfterDataStreamSource{
		events: [][]byte{[]byte(`data: {"type":"message_start","usage":{"input_tokens":5}}`)},
		err:    errors.New("connection reset by peer"),
	}
	writer := newMockStreamWriter()
	finishCalls := 0
	var finished []byte
	processor := NewStreamProcessor(StreamConfig{
		Source:          source,
		Writer:          writer,
		Context:         context.Background(),
		BufferRawStream: true,
		OnFinish: func(_ context.Context, rawStream []byte) error {
			finishCalls++
			finished = rawStream
			return nil
		},
	})

	err := processor.Run()
	if err == nil || !strings.Contains(err.Error(), "stream read error") {
		t.Fatalf("expected read error, got %v", err)
	}
	if finishCalls != 1 {
		t.Fatalf("OnFinish called %d times on read error, want 1", finishCalls)
	}
	if !strings.Contains(string(finished), "message_start") {
		t.Fatalf("partial stream content lost on read error: %q", finished)
	}
}

// F16：上游 SSE 心跳（纯注释块）原样透传但不计为首 token/有效载荷。
func TestStreamProcessor_PassthroughHeartbeatNotFirstToken(t *testing.T) {
	source := newMockStreamSource([][]byte{
		[]byte(": keep-alive\n\n"),
		[]byte(": ping\n\n"),
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"),
	})
	writer := newMockStreamWriter()
	firstTokenFired := 0
	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: context.Background(),
		OnFirstToken: func() {
			firstTokenFired++
		},
	})
	if err := processor.Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if firstTokenFired != 1 {
		t.Fatalf("OnFirstToken fired %d times, want 1 (heartbeat chunks must not count)", firstTokenFired)
	}
	if !processor.PayloadWritten() {
		t.Fatal("real payload must mark payloadWritten")
	}
}

// F16：心跳与真实数据混合的块（字节切分边界）仍按有效载荷计。
func TestStreamProcessor_MixedHeartbeatChunkCountsAsFirstToken(t *testing.T) {
	source := newMockStreamSource([][]byte{
		[]byte(": keep-alive\n\ndata: {\"delta\":\"hi\"}\n\n"),
	})
	writer := newMockStreamWriter()
	firstTokenFired := 0
	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: context.Background(),
		OnFirstToken: func() {
			firstTokenFired++
		},
	})
	if err := processor.Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if firstTokenFired != 1 {
		t.Fatalf("mixed chunk must count as first token, fired %d", firstTokenFired)
	}
}

// F16：整条流只有心跳 = 未交付任何真实内容，按空流处理（可 failover）。
func TestStreamProcessor_HeartbeatOnlyStreamIsEmpty(t *testing.T) {
	source := newMockStreamSource([][]byte{
		[]byte(": keep-alive\n\n"),
		[]byte(": ping\n\n"),
	})
	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  newMockStreamWriter(),
		Context: context.Background(),
	})
	if err := processor.Run(); !errors.Is(err, ErrEmptyUpstreamStream) {
		t.Fatalf("heartbeat-only stream should be ErrEmptyUpstreamStream, got %v", err)
	}
}

func TestIsSSECommentOnly(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{"comment block", []byte(": keep-alive\n\n"), true},
		{"comment then empty lines", []byte(": ping\n\n\n"), true},
		{"comment split mid-line", []byte(": pi"), true},
		{"data line present", []byte(": ping\n\ndata: {}"), false},
		{"pure data", []byte("data: {\"a\":1}\n\n"), false},
		{"empty payload", nil, false},
		{"whitespace only", []byte("\n\n"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSSECommentOnly(tc.payload); got != tc.want {
				t.Fatalf("isSSECommentOnly(%q) = %v, want %v", tc.payload, got, tc.want)
			}
		})
	}
}
