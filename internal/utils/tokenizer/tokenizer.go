package tokenizer

import (
	"sync"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/tiktoken-go/tokenizer/codec"
)

// maxAccurateCountBytes 限制走 BPE 精确计数的输入上限。超过阈值的请求体
// （长上下文、含 base64 图片）改用 len/4 粗估：这类计数只服务 usage 缺失时
// 的兜底估算，不值得为它付出数百毫秒的 BPE 开销（base64 长跑对 BPE 接近 O(n²)）。
const maxAccurateCountBytes = 64 * 1024

// encoder 必须全局单例：codec 的词表有 sync.Once 缓存，但分割正则在每次
// NewO200kBase 时都重新编译，热路径逐请求重建的代价不可接受。
var encoder = sync.OnceValue(func() *codec.Codec {
	return codec.NewO200kBase()
})

func CountTokens(content, model string) int {
	// TODO 更多模型
	if len(content) > maxAccurateCountBytes {
		return len(content) / 4
	}
	tc, err := encoder().Count(content)
	if err != nil {
		log.Warnf("tokenizer.CountTokens failed, using len/4 estimate: %v", err)
		return len(content) / 4
	}
	return tc
}

// CountTokensBytes 与 CountTokens 相同，但超阈值时不做 []byte→string 的
// 全量拷贝，直接按字节长度粗估。
func CountTokensBytes(payload []byte, model string) int {
	// TODO 更多模型
	if len(payload) > maxAccurateCountBytes {
		return len(payload) / 4
	}
	tc, err := encoder().Count(string(payload))
	if err != nil {
		log.Warnf("tokenizer.CountTokensBytes failed, using len/4 estimate: %v", err)
		return len(payload) / 4
	}
	return tc
}
