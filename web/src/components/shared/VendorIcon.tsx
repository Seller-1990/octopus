import { Brain } from 'lucide-react';
import {
    OpenAI,
    Claude,
    DeepSeek,
    Grok,
    Qwen,
    Meta,
    Mistral,
    Minimax,
    Kimi,
    Zhipu,
    Doubao,
    Wenxin,
    Hunyuan,
    Cohere,
    Perplexity,
    Aws,
    Microsoft,
    Nvidia,
    Google,
} from '@lobehub/icons';
import { cn } from '@/lib/utils';

type AvatarComponent = typeof OpenAI.Avatar;

const VENDOR_ICON_MAP: Record<string, AvatarComponent> = {
    openai: OpenAI.Avatar,
    anthropic: Claude.Avatar,
    google: Google.Avatar,
    deepseek: DeepSeek.Avatar,
    xai: Grok.Avatar,
    alibaba: Qwen.Avatar,
    meta: Meta.Avatar,
    mistral: Mistral.Avatar,
    minimax: Minimax.Avatar,
    moonshotai: Kimi.Avatar,
    zhipuai: Zhipu.Avatar,
    bytedance: Doubao.Avatar,
    baidu: Wenxin.Avatar,
    tencent: Hunyuan.Avatar,
    cohere: Cohere.Avatar,
    perplexity: Perplexity.Avatar,
    amazon: Aws.Avatar,
    microsoft: Microsoft.Avatar,
    nvidia: Nvidia.Avatar,
};

export function VendorIcon({ vendor, className }: { vendor: string; className?: string }) {
    const Avatar = VENDOR_ICON_MAP[vendor.toLowerCase()];

    if (Avatar) {
        return <Avatar size={16} className={cn('size-4', className)} />;
    }

    return <Brain className={cn('size-4 text-muted-foreground', className)} />;
}
