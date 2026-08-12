package visionbridge

import (
	"strings"
	"testing"
	"time"
)

func TestBuildPromptLanguageAndTruncation(t *testing.T) {
	zh := BuildPrompt("auto", "帮我看看这张图里的代码", 1)
	if !strings.Contains(zh, "图片描述助手") {
		t.Fatal("CJK hint should resolve to zh template")
	}
	en := BuildPrompt("auto", "what is in this image", 2)
	if !strings.Contains(en, "image description assistant") {
		t.Fatal("non-CJK hint should resolve to en template")
	}
	forced := BuildPrompt("en", "中文问题", 1)
	if !strings.Contains(forced, "image description assistant") {
		t.Fatal("explicit language must override auto detection")
	}

	long := strings.Repeat("喵", 3000)
	p := BuildPrompt("zh", long, 1)
	if strings.Count(p, "喵") != maxFocusHintRunes {
		t.Fatalf("focus hint not truncated to %d runes", maxFocusHintRunes)
	}
	if strings.Count(p, "---") < 2 {
		t.Fatal("focus hint must be wrapped by --- separators")
	}
}

func TestBuildPromptEmptyHint(t *testing.T) {
	p := BuildPrompt("zh", "   ", 1)
	if !strings.Contains(p, "无特定问题") {
		t.Fatal("empty hint should fall back to generic instruction")
	}
}

func TestAnalysisCacheLRUAndTTL(t *testing.T) {
	c := newAnalysisCache(2)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }

	c.Set("a", "A", time.Minute)
	c.Set("b", "B", time.Minute)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should be cached")
	}
	// a 刚被访问过，插入 c 应淘汰 b
	c.Set("c", "C", time.Minute)
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should be evicted (LRU)")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should survive eviction")
	}

	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be expired by TTL")
	}
}

func TestAnalysisCacheZeroTTLNotStored(t *testing.T) {
	c := newAnalysisCache(2)
	c.Set("k", "v", 0)
	if _, ok := c.Get("k"); ok {
		t.Fatal("zero TTL must not be stored")
	}
}
