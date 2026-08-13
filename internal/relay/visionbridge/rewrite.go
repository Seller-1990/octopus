package visionbridge

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// RewriteRequest 返回把全部图片块替换为文本描述后的新请求。
// 原请求不被修改（跨通道共享，vision 通道仍需原图）：请求结构浅拷贝，
// Tools 与全部消息的内容块数组重建（隔离出站转换的就地压缩）。
//
// 替换规则（Step 0a 验证的格式）：每个图片块原位替换为 "[Image N]" 标记，
// 联合分析文本追加在最后一个图片块的位置之后，编号与 VLM 提示词中的顺序一致。
func RewriteRequest(req *model.InternalLLMRequest, refs []ImageRef, analysis string) (*model.InternalLLMRequest, error) {
	if len(refs) == 0 {
		return nil, fmt.Errorf("no image refs to rewrite")
	}
	// refs 按文档顺序排列（Discover 顺序扫描），全局编号直接取下标+1，
	// 再按消息聚合成 局部块下标 → 全局编号 的映射。
	partNosByMsg := make(map[int]map[int]int, len(refs))
	for i, ref := range refs {
		if partNosByMsg[ref.MessageIndex] == nil {
			partNosByMsg[ref.MessageIndex] = make(map[int]int)
		}
		partNosByMsg[ref.MessageIndex][ref.PartIndex] = i + 1
	}
	last := refs[len(refs)-1]

	rewritten := *req
	// 出站转换会就地压缩切片（Normalize / FlattenUnsupportedBlocks 的 [:0] 复用），
	// 副本与原请求不得共享任何会被压缩的底层数组：Tools 与每条消息的内容块全部重建，
	// 否则对副本的转换会把原请求（vision 通道仍要用）改出重复元素。
	rewritten.Tools = append([]model.Tool(nil), req.Tools...)
	rewritten.Messages = make([]model.Message, len(req.Messages))
	copy(rewritten.Messages, req.Messages)

	// Responses 出站以 RawInputItems / 扩展 RawResponseItems 为权威输入源（优先于
	// Messages），它们携带原图——副本必须清空两级，让上游载荷从改写后的 Messages 重建。
	// ProviderExtensions 是共享指针，先克隆容器再清，不得动原请求（vision 通道仍需原图）。
	rewritten.RawInputItems = nil
	if req.ProviderExtensions != nil {
		extCopy := *req.ProviderExtensions
		if extCopy.OpenAI != nil {
			openaiCopy := *extCopy.OpenAI
			openaiCopy.RawResponseItems = nil
			extCopy.OpenAI = &openaiCopy
		}
		rewritten.ProviderExtensions = &extCopy
	}

	for i := range rewritten.Messages {
		partNos, hasImage := partNosByMsg[i]
		if !hasImage {
			if parts := rewritten.Messages[i].Content.MultipleContent; parts != nil {
				rewritten.Messages[i].Content.MultipleContent = append([]model.MessageContentPart(nil), parts...)
			}
			continue
		}
		joint := ""
		if i == last.MessageIndex {
			joint = jointAnalysisBlock(len(refs), analysis)
		}
		rewritten.Messages[i] = rewriteMessage(req.Messages[i], partNos, last, joint)
	}

	if rewritten.HasImages() {
		return nil, fmt.Errorf("rewrite left residual image_url blocks (fail-closed)")
	}
	return &rewritten, nil
}

// rewriteMessage 深拷贝单条消息的内容块并完成替换。
// partNos: 该消息内 图片块下标 → 全局图片编号；joint 非空时追加在 last 对应块之后。
func rewriteMessage(msg model.Message, partNos map[int]int, last ImageRef, joint string) model.Message {
	parts := msg.Content.MultipleContent
	newParts := make([]model.MessageContentPart, 0, len(parts)+1)
	for j := range parts {
		no, isImage := partNos[j]
		if !isImage {
			newParts = append(newParts, parts[j])
			continue
		}
		marker := fmt.Sprintf("[Image %d]", no)
		newParts = append(newParts, model.MessageContentPart{Type: "text", Text: &marker})
		if joint != "" && j == last.PartIndex {
			block := joint
			newParts = append(newParts, model.MessageContentPart{Type: "text", Text: &block})
		}
	}
	msg.Content = model.MessageContent{MultipleContent: newParts}
	return msg
}

func jointAnalysisBlock(imageCount int, analysis string) string {
	if imageCount == 1 {
		return fmt.Sprintf("[Image 1 — Visual analysis]\n%s", analysis)
	}
	return fmt.Sprintf("[Images 1-%d — Joint visual analysis]\n%s", imageCount, analysis)
}
