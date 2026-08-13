package model

// HasImages 报告请求的任一消息（含 tool 角色消息）是否携带 image_url 内容块。
// 同时扫描 Message.Images（客户端可写、OpenAI 出站会原样带出的旁路字段）。
// vision bridge 以此作为触发前置条件之一；判断只读不修改。
func (r *InternalLLMRequest) HasImages() bool {
	if r == nil {
		return false
	}
	for i := range r.Messages {
		for j := range r.Messages[i].Content.MultipleContent {
			part := &r.Messages[i].Content.MultipleContent[j]
			if part.Type == "image_url" && part.ImageURL != nil && part.ImageURL.URL != "" {
				return true
			}
		}
		for j := range r.Messages[i].Images {
			part := &r.Messages[i].Images[j]
			if part.Type == "image_url" && part.ImageURL != nil && part.ImageURL.URL != "" {
				return true
			}
		}
	}
	return false
}
