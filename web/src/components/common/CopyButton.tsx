'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { Check, Copy } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';
import { toast } from '@/components/common/Toast';

export type CopyIconButtonProps = {
    text: string;
    /**
     * 可选的异步取文本函数，优先于 text。
     * 用于文本需要按需请求的场景（如复制后端掩码存储的完整 API Key）。
     */
    getText?: () => Promise<string | undefined>;
    className?: string;
    copyIconClassName?: string;
    checkIconClassName?: string;
};

/**
 * 复制到剪贴板，带非安全上下文回退。
 * Octopus 常以 HTTP 内网地址（如 http://192.168.50.139:8088）访问，
 * 此时 navigator.clipboard 不存在（仅 HTTPS/localhost 可用），异步
 * API 直接不可用；退回临时 textarea + execCommand 的同步路径。
 */
export async function writeClipboard(value: string): Promise<void> {
    if (navigator.clipboard?.writeText) {
        try {
            await navigator.clipboard.writeText(value);
            return;
        } catch {
            // 权限被拒或文档失焦：退回 execCommand。
        }
    }
    const textarea = document.createElement('textarea');
    textarea.value = value;
    textarea.setAttribute('readonly', '');
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.select();
    try {
        if (!document.execCommand('copy')) {
            throw new Error('execCommand copy rejected');
        }
    } finally {
        textarea.remove();
    }
}

export function CopyIconButton({
    text,
    getText,
    className,
    copyIconClassName,
    checkIconClassName,
}: CopyIconButtonProps) {
    const t = useTranslations('common.copy');
    const [copied, setCopied] = useState(false);
    const timerRef = useRef<number | null>(null);

    useEffect(() => {
        return () => {
            if (timerRef.current) window.clearTimeout(timerRef.current);
        };
    }, []);

    const handleClick = useCallback(async () => {
        let resolved: string | undefined = text;
        try {
            if (getText) {
                resolved = await getText();
            }
        } catch (err) {
            const description = err instanceof Error ? err.message : String(err);
            toast.error(t('failed'), { description });
            return;
        }
        if (!resolved) {
            toast.error(t('failed'), { description: t('noContent') });
            return;
        }

        try {
            await writeClipboard(resolved);

            setCopied(true);
            toast.success(t('success'));

            if (timerRef.current) window.clearTimeout(timerRef.current);
            timerRef.current = window.setTimeout(() => setCopied(false), 2000);
        } catch (err) {
            const description = err instanceof Error ? err.message : String(err);
            toast.error(t('failed'), { description });
        }
    }, [
        text,
        getText,
        t,
    ]);

    return (
        <button
            type="button"
            onClick={handleClick}
            aria-label={copied ? t('success') : t('action')}
            title={copied ? t('success') : t('action')}
            className={cn(className)}
        >
            <AnimatePresence mode="wait" initial={false}>
                {copied ? (
                    <motion.div key="check" initial={{ scale: 0 }} animate={{ scale: 1 }} exit={{ scale: 0 }}>
                        <Check className={cn(checkIconClassName)} />
                    </motion.div>
                ) : (
                    <motion.div key="copy" initial={{ scale: 0 }} animate={{ scale: 1 }} exit={{ scale: 0 }}>
                        <Copy className={cn(copyIconClassName)} />
                    </motion.div>
                )}
            </AnimatePresence>
        </button>
    );
}

