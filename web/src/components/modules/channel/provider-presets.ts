import { ChannelType } from '@/api/endpoints/channel';

export type ProviderPresetGroup = 'official' | 'china' | 'global' | 'local';

export interface ProviderPreset {
    id: string;
    /** 品牌名为专有名词，不做 i18n；中文仅为提示 */
    name: string;
    type: ChannelType;
    /** 直接可用的 base_url（含 /v1 等版本前缀，与各出站适配器的路径拼接规则匹配） */
    baseUrl: string;
    group: ProviderPresetGroup;
}

// 调研基线（2026-09 核对官方文档）：所有列出的提供商均提供 OpenAI 兼容或既有
// 适配器协议端点，无需新增 OutboundType。注意事项：
// 1. base_url 与后端拼接规则组合后的完整路径（openai 系追加 /chat/completions、
//    anthropic 系追加 /messages、gemini 自动补版本段）
// 2. 「拉取模型」(base + /models) 并非所有提供商都有——deepseek-anthropic、
//    volcengine(Responses) 未证实有模型列表端点，这些预设需手填模型
// 3. 本地组以 Octopus 服务端进程视角解析：容器部署时 127.0.0.1 指向容器自身，
//    需改为 host.docker.internal 或局域网 IP
export const PROVIDER_PRESETS: ProviderPreset[] = [
    // 官方
    { id: 'openai', name: 'OpenAI', type: ChannelType.OpenAIChat, baseUrl: 'https://api.openai.com/v1', group: 'official' },
    { id: 'openai-responses', name: 'OpenAI (Responses API)', type: ChannelType.OpenAIResponse, baseUrl: 'https://api.openai.com/v1', group: 'official' },
    { id: 'anthropic', name: 'Anthropic', type: ChannelType.Anthropic, baseUrl: 'https://api.anthropic.com/v1', group: 'official' },
    // chat 适配器与 fetch-models(Gemini 分支) 均可处理带 /v1beta 的 base，
    // 且 fetch 已与 chat 同口径支持裸主机回退（helper/fetch.go G-H5）
    { id: 'gemini', name: 'Google Gemini', type: ChannelType.Gemini, baseUrl: 'https://generativelanguage.googleapis.com/v1beta', group: 'official' },

    // 国内
    { id: 'deepseek', name: 'DeepSeek (深度求索)', type: ChannelType.OpenAIChat, baseUrl: 'https://api.deepseek.com', group: 'china' },
    { id: 'deepseek-anthropic', name: 'DeepSeek (Anthropic 协议)', type: ChannelType.Anthropic, baseUrl: 'https://api.deepseek.com/anthropic/v1', group: 'china' },
    { id: 'moonshot', name: 'Moonshot AI (Kimi)', type: ChannelType.OpenAIChat, baseUrl: 'https://api.moonshot.cn/v1', group: 'china' },
    { id: 'zhipu', name: 'Zhipu GLM (智谱)', type: ChannelType.OpenAIChat, baseUrl: 'https://open.bigmodel.cn/api/paas/v4', group: 'china' },
    { id: 'minimax-cn', name: 'MiniMax (国内)', type: ChannelType.OpenAIChat, baseUrl: 'https://api.minimaxi.com/v1', group: 'china' },
    { id: 'dashscope', name: 'DashScope (阿里百炼·兼容模式)', type: ChannelType.OpenAIChat, baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', group: 'china' },
    { id: 'qianfan', name: 'Qianfan (百度千帆 v2)', type: ChannelType.OpenAIChat, baseUrl: 'https://qianfan.baidubce.com/v2', group: 'china' },
    { id: 'hunyuan', name: 'Hunyuan (腾讯混元)', type: ChannelType.OpenAIChat, baseUrl: 'https://api.hunyuan.cloud.tencent.com/v1', group: 'china' },
    { id: 'volcengine', name: 'Volcengine Ark (火山方舟/豆包)', type: ChannelType.Volcengine, baseUrl: 'https://ark.cn-beijing.volces.com/api/v3', group: 'china' },
    { id: 'siliconflow', name: 'SiliconFlow (硅基流动)', type: ChannelType.OpenAIChat, baseUrl: 'https://api.siliconflow.cn/v1', group: 'china' },

    // 国际
    { id: 'opencoder', name: 'OpenCode Zen', type: ChannelType.OpenAIChat, baseUrl: 'https://opencode.ai/zen/v1', group: 'global' },
    { id: 'openrouter', name: 'OpenRouter', type: ChannelType.OpenAIChat, baseUrl: 'https://openrouter.ai/api/v1', group: 'global' },
    { id: 'groq', name: 'Groq', type: ChannelType.OpenAIChat, baseUrl: 'https://api.groq.com/openai/v1', group: 'global' },
    { id: 'xai', name: 'xAI (Grok)', type: ChannelType.OpenAIChat, baseUrl: 'https://api.x.ai/v1', group: 'global' },
    { id: 'minimax-global', name: 'MiniMax (国际)', type: ChannelType.OpenAIChat, baseUrl: 'https://api.minimax.io/v1', group: 'global' },
    { id: 'mistral', name: 'Mistral', type: ChannelType.OpenAIChat, baseUrl: 'https://api.mistral.ai/v1', group: 'global' },
    { id: 'together', name: 'Together AI', type: ChannelType.OpenAIChat, baseUrl: 'https://api.together.xyz/v1', group: 'global' },
    { id: 'fireworks', name: 'Fireworks AI', type: ChannelType.OpenAIChat, baseUrl: 'https://api.fireworks.ai/inference/v1', group: 'global' },
    { id: 'cerebras', name: 'Cerebras', type: ChannelType.OpenAIChat, baseUrl: 'https://api.cerebras.ai/v1', group: 'global' },

    // 本地
    { id: 'ollama', name: 'Ollama', type: ChannelType.OpenAIChat, baseUrl: 'http://127.0.0.1:11434/v1', group: 'local' },
    { id: 'lmstudio', name: 'LM Studio', type: ChannelType.OpenAIChat, baseUrl: 'http://127.0.0.1:1234/v1', group: 'local' },
    { id: 'vllm', name: 'vLLM', type: ChannelType.OpenAIChat, baseUrl: 'http://127.0.0.1:8000/v1', group: 'local' },
];

export const PRESET_GROUP_ORDER: ProviderPresetGroup[] = ['official', 'china', 'global', 'local'];
