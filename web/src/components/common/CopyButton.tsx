'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { Check, Copy } from 'lucide-react';
import { useCopyToClipboard } from '@uidotdev/usehooks';
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

export function CopyIconButton({
    text,
    getText,
    className,
    copyIconClassName,
    checkIconClassName,
}: CopyIconButtonProps) {
    const t = useTranslations('common.copy');
    const [, copyToClipboard] = useCopyToClipboard();
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
            await copyToClipboard(resolved);

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
        copyToClipboard,
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

