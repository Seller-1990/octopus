package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func withWebhookLoader(t *testing.T, url, events string, enabled bool, hits *atomic.Int32, received chan<- Event) {
	t.Helper()
	SetWebhookLoader(func() (string, string, bool) { return url, events, enabled })
	t.Cleanup(func() { SetWebhookLoader(nil) })
	if url == "" {
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode webhook body: %v", err)
		}
		if hits != nil {
			hits.Add(1)
		}
		if received != nil {
			received <- event
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	// 用注入的 url 覆盖为测试服务器地址：loader 闭包再包一层
	loader := WebhookConfigLoader
	SetWebhookLoader(func() (string, string, bool) {
		_, events, enabled := loader()
		return server.URL, events, enabled
	})
}

func TestWebhookDeliversEvent(t *testing.T) {
	hits := &atomic.Int32{}
	received := make(chan Event, 1)
	withWebhookLoader(t, "http://example.invalid", "", true, hits, received)

	Default.Publish(Event{Type: TypeSiteBatch, Level: LevelSuccess, Title: "批量同步完成"})
	select {
	case got := <-received:
		if got.Type != TypeSiteBatch || got.Title != "批量同步完成" {
			t.Fatalf("webhook payload wrong: %+v", got)
		}
		if hits.Load() != 1 {
			t.Fatalf("hits = %d, want 1", hits.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook not called")
	}
}

func TestWebhookDisabledOrEmptyURLSkips(t *testing.T) {
	hits := &atomic.Int32{}
	withWebhookLoader(t, "", "", true, hits, nil)
	Default.Publish(Event{Type: TypeSiteBatch, Title: "no url"})
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatal("empty url must not post")
	}

	withWebhookLoader(t, "http://example.invalid", "", false, hits, nil)
	Default.Publish(Event{Type: TypeSiteBatch, Title: "disabled"})
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatal("disabled must not post")
	}
}

func TestWebhookEventFilter(t *testing.T) {
	hits := &atomic.Int32{}
	received := make(chan Event, 1)
	withWebhookLoader(t, "http://example.invalid", TypeSiteCheckinFailed, true, hits, received)

	Default.Publish(Event{Type: TypeSiteBatch, Title: "filtered out"})
	Default.Publish(Event{Type: TypeSiteCheckinFailed, Level: LevelError, Title: "签到失败"})
	select {
	case got := <-received:
		if got.Type != TypeSiteCheckinFailed {
			t.Fatalf("filtered webhook got %s", got.Type)
		}
		if hits.Load() != 1 {
			t.Fatalf("hits = %d, want 1 (non-matching events filtered)", hits.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("matching event not delivered")
	}
	// 给被过滤事件留出误投递窗口
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 1 {
		t.Fatalf("filtered event must not post, hits = %d", hits.Load())
	}
}
