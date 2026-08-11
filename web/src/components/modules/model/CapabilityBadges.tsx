'use client';

import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';

/** 能力徽标定义（与后端 model.CapabilityNames 对应）。 */
interface CapabilityDef {
    key: string;
    icon: string;
    labelKey: string;
    cls: string;
}

const CAPABILITY_DEFS: Record<string, CapabilityDef> = {
    multimodal: { key: 'multimodal', icon: '🖼️', labelKey: 'capability.multimodal', cls: 'bg-violet-500/15 text-violet-700 dark:text-violet-300' },
    reasoning: { key: 'reasoning', icon: '🧠', labelKey: 'capability.reasoning', cls: 'bg-blue-500/15 text-blue-700 dark:text-blue-300' },
    voice_input: { key: 'voice_input', icon: '🎤', labelKey: 'capability.voice_input', cls: 'bg-cyan-500/15 text-cyan-700 dark:text-cyan-300' },
    voice_output: { key: 'voice_output', icon: '🔊', labelKey: 'capability.voice_output', cls: 'bg-cyan-500/15 text-cyan-700 dark:text-cyan-300' },
    image_gen: { key: 'image_gen', icon: '🎨', labelKey: 'capability.image_gen', cls: 'bg-pink-500/15 text-pink-700 dark:text-pink-300' },
    video_gen: { key: 'video_gen', icon: '🎬', labelKey: 'capability.video_gen', cls: 'bg-amber-500/15 text-amber-700 dark:text-amber-300' },
};

const VOICE_ICON = '🎤';

/**
 * 模型能力徽标（models.dev 静态声明，只读）。
 * size: sm=emoji+文字（目录卡片）；xs=仅 emoji（发现行/分组卡片，tooltip 见文）。
 * 语音输入/输出合并为单个 🎤 徽标（tooltip 区分方向）。
 */
export function CapabilityBadges({
    capabilities,
    size = 'sm',
    className,
}: {
    capabilities?: string[];
    size?: 'sm' | 'xs';
    className?: string;
}) {
    const t = useTranslations('model.catalog');
    if (!capabilities || capabilities.length === 0) return null;

    // 语音输入/输出合并为单个 🎤（用 voice_input 的 tooltip，标注双向）
    const voice = capabilities.includes('voice_input') || capabilities.includes('voice_output');
    const others = capabilities.filter((c) => c !== 'voice_input' && c !== 'voice_output');

    const renderBadge = (cap: CapabilityDef, tooltip: string) => (
        <span
            key={cap.key}
            title={tooltip}
            aria-label={tooltip}
            className={cn(
                'inline-flex shrink-0 items-center rounded leading-none',
                cap.cls,
                size === 'sm' ? 'px-1 py-px text-[10px] font-medium gap-0.5' : 'px-0.5 py-px text-[9px]',
                className,
            )}
        >
            <span aria-hidden="true">{cap.icon}</span>
            {size === 'sm' && <span className="font-medium">{t(cap.labelKey as never)}</span>}
        </span>
    );

    const voiceTooltip = capabilities.includes('voice_input') && capabilities.includes('voice_output')
        ? `${t('capability.voice_input')} + ${t('capability.voice_output')}`
        : capabilities.includes('voice_input') ? t('capability.voice_input') : t('capability.voice_output');

    return (
        <span className="inline-flex items-center gap-0.5">
            {voice && renderBadge({ key: 'voice', icon: VOICE_ICON, labelKey: 'capability.voice_input', cls: CAPABILITY_DEFS.voice_input.cls }, voiceTooltip)}
            {others.map((c) => {
                const def = CAPABILITY_DEFS[c];
                return def ? renderBadge(def, t(def.labelKey as never)) : null;
            })}
        </span>
    );
}
