package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

// wsUpstreamReader reads events from an upstream WebSocket connection.
// closed/reading/statusCode 会被读 goroutine（ReadEvent）与主 goroutine
// （Close/CloseWithError/StatusCode）交叉访问，必须走 atomic。
type wsUpstreamReader struct {
	conn       *websocket.Conn
	pc         *pooledConn
	channelID  int
	keyID      int
	closed     atomic.Bool
	reading    atomic.Bool // ReadEvent 在途标记，Close 据此决定回池还是弃用
	done       bool        // 仅读 goroutine 访问：true after a terminal event has been returned
	statusCode atomic.Int32
}

func newWSUpstreamReader(pc *pooledConn, channelID, keyID int) *wsUpstreamReader {
	r := &wsUpstreamReader{
		conn:      pc.conn,
		pc:        pc,
		channelID: channelID,
		keyID:     keyID,
	}
	r.statusCode.Store(200)
	return r
}

func (r *wsUpstreamReader) ReadEvent(ctx context.Context) ([]byte, error) {
	if r.closed.Load() || r.done {
		return nil, io.EOF
	}
	// 先置在途标记再继续：与 Close 的交错顺序保证——若 Close 在读进入前完成，
	// 这里的 closed 复查会退出；若读已在途，Close 会弃用连接而非回池。
	r.reading.Store(true)
	defer r.reading.Store(false)

	msgType, data, err := r.conn.Read(ctx)
	if err != nil {
		// Check if it's a normal close
		closeStatus := websocket.CloseStatus(err)
		if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway {
			return nil, io.EOF
		}
		switch closeStatus {
		case websocket.StatusPolicyViolation:
			r.statusCode.Store(http.StatusConflict)
		case websocket.StatusTryAgainLater:
			r.statusCode.Store(http.StatusServiceUnavailable)
		default:
			if r.statusCode.Load() < 400 {
				r.statusCode.Store(http.StatusBadGateway)
			}
		}
		return nil, fmt.Errorf("ws read error: %w", err)
	}

	if msgType != websocket.MessageText {
		return nil, fmt.Errorf("unexpected ws message type: %d", msgType)
	}

	// Check for error and terminal events.
	var event struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Response *struct {
			Status string `json:"status"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &event) == nil {
		if isWSStreamTerminalEvent(event.Type) {
			r.done = true
		}
		if event.Response != nil && (event.Response.Status == "failed" || event.Response.Status == "incomplete" || event.Response.Status == "cancelled" || event.Response.Status == "canceled") {
			r.done = true
		}
		if isWSStreamErrorEvent(event.Type) || event.Error != nil || (event.Response != nil && event.Response.Error != nil) {
			r.done = true
			if event.Status > 0 {
				r.statusCode.Store(int32(event.Status))
			} else if r.statusCode.Load() < 400 {
				r.statusCode.Store(http.StatusBadGateway)
			}
			// Semantic error/terminal frames must reach the transformer and
			// downstream client. Returning an error here used to swallow
			// response.failed after partial output and lose its usage.
		}
	}

	return data, nil
}

func (r *wsUpstreamReader) StatusCode() int {
	return int(r.statusCode.Load())
}

func (r *wsUpstreamReader) Headers() http.Header {
	return http.Header{
		"Content-Type": []string{"text/event-stream"},
	}
}

func (r *wsUpstreamReader) Body() io.ReadCloser {
	return nil // WS doesn't have a body
}

func (r *wsUpstreamReader) Close() error {
	if r.closed.Swap(true) {
		return nil
	}
	// 读仍在途（ctx 取消尚未传播完）：连接可能停在消息中间，被取消打断的
	// 帧不可恢复，复用会让下一个请求读到错乱的帧——弃用而非回池。
	if r.reading.Load() {
		wsUpstreamPool.RemoveConn(r.pc)
		log.Debugf("upstream WS connection dropped (read still in flight, channel=%d, key=%d)", r.channelID, r.keyID)
		return nil
	}
	// Return connection to pool (don't close it)
	wsUpstreamPool.Put(r.pc)
	log.Debugf("upstream WS connection returned to pool (channel=%d, key=%d)", r.channelID, r.keyID)
	return nil
}

// CloseWithError closes the reader and removes the connection from pool.
func (r *wsUpstreamReader) CloseWithError() {
	if r.pc == nil {
		return
	}
	r.closed.Store(true)
	wsUpstreamPool.RemoveConn(r.pc)
}
