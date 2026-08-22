'use client';

import { useEffect, useId, useRef, useState } from 'react';
import { Layers, GripVertical, X, Trash2, Coins, Wallet, FlaskConical, Ban, RotateCcw } from 'lucide-react';
import {
    DragDropContext,
    Draggable,
    Droppable,
    type DraggableProvided,
    type DropResult,
} from '@hello-pangea/dnd';
import { motion, AnimatePresence } from 'motion/react';
import { cn } from '@/lib/utils';
import { getModelIcon } from '@/lib/model-icons';
import type { LLMChannel } from '@/api/endpoints/model';
import { CapabilityBadges } from '@/components/modules/model/CapabilityBadges';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { Badge } from '@/components/ui/badge';
import { useTranslations } from 'next-intl';

function formatBalance(value: number): string {
    if (!Number.isFinite(value)) return '-';
    if (Math.abs(value) >= 1000) {
        const scaled = value / 1000;
        const text = scaled >= 100 ? Math.round(scaled).toString() : (Math.round(scaled * 10) / 10).toString();
        return `$${text}k`;
    }
    return `$${value.toFixed(2)}`;
}

function formatMultiplier(value: number): string {
    if (!Number.isFinite(value)) return '-';
    return `${Math.round(value * 100) / 100}x`;
}

export interface SelectedMember extends LLMChannel {
    id: string;
    item_id?: number;
    weight?: number;
    group_multiplier?: number | null;
    effective_multiplier?: number | null;
    multiplier_source?: string;
    multiplier_cap?: number | null;
    multiplier_known?: boolean | null;
    supports_tools?: boolean | null;
    supports_tools_source?: string;
    supports_tools_probed_at?: string;
    policy_status?: string;
    policy_reason?: string;
}

// 不可用 key：渠道被禁用，或非免费分组的绑定账号余额低于后端预检阈值（≤0.1）。
// 与 relay.Handler 的 isFreeGroupItem / balance <= 0.1 保持一致。
export function isUnavailableMember(member: SelectedMember): boolean {
    const disabled = member.enabled === false;
    const free = member.multiplier_known === true && member.group_multiplier === 0;
    const balanceInsufficient = !free && member.balance != null && member.balance <= 0.1;
    return disabled || balanceInsufficient;
}

function reorderList<T>(list: T[], startIndex: number, endIndex: number): T[] {
    const result = [...list];
    const [removed] = result.splice(startIndex, 1);
    result.splice(endIndex, 0, removed);
    return result;
}

type MemberItemDnd = {
    innerRef: DraggableProvided['innerRef'];
    draggableProps: DraggableProvided['draggableProps'];
    dragHandleProps: DraggableProvided['dragHandleProps'];
    isDragging: boolean;
};

function MemberItem({
    member,
    onRemove,
    onWeightChange,
    isRemoving,
    index,
    showWeight = false,
    showConfirmDelete = true,
    layoutScope,
    dnd,
    onToolsTest,
    onToolsForce,
    onToolsReset,
    toolsDisabled = false,
}: {
    member: SelectedMember;
    onRemove: (id: string) => void;
    onWeightChange?: (id: string, weight: number) => void;
    isRemoving?: boolean;
    index: number;
    showWeight?: boolean;
    showConfirmDelete?: boolean;
    layoutScope?: string;
    dnd: MemberItemDnd;
    onToolsTest?: (member: SelectedMember) => void;
    onToolsForce?: (member: SelectedMember) => void;
    onToolsReset?: (member: SelectedMember) => void;
    toolsDisabled?: boolean;
}) {
    const { Avatar: ModelAvatar } = getModelIcon(member.name);
    const [confirmDelete, setConfirmDelete] = useState(false);
    const t = useTranslations('group');
    const isDisabled = member.enabled === false;
    const isFreeMember = member.multiplier_known === true && member.group_multiplier === 0;
    const isBalanceInsufficient = !isFreeMember && member.balance != null && member.balance <= 0.1;
    const isUnavailable = isDisabled || isBalanceInsufficient;
    const isSiteChannel = member.site_id != null;
    const sourceLabel = [member.channel_name, isSiteChannel ? null : member.endpoint_type?.trim()]
        .filter(Boolean)
        .join(' · ');
    const canForce = member.supports_tools !== false;
    const canReset = member.supports_tools === false;

    return (
        <div
            // DnD libraries provide imperative refs/props; the hook lint rule (`react-hooks/refs`)
            // flags this pattern, but it's safe and required for correct drag behavior.
            // eslint-disable-next-line react-hooks/refs
            ref={dnd.innerRef}
            // eslint-disable-next-line react-hooks/refs
            {...dnd.draggableProps}
            className={cn('rounded-lg grid transition-[grid-template-rows] duration-200', isRemoving ? 'grid-rows-[0fr]' : 'grid-rows-[1fr]')}
            // eslint-disable-next-line react-hooks/refs
            style={{
                /* eslint-disable-next-line react-hooks/refs */
                ...(dnd.draggableProps?.style ?? {}),
                /* eslint-disable-next-line react-hooks/refs */
                ...(dnd.isDragging ? { zIndex: 50, boxShadow: '0 8px 32px rgba(0,0,0,0.15)' } : null),
            }}
        >
            <div className={cn(
                'group flex items-center gap-2 rounded-lg bg-background border border-border/50 px-2.5 py-2 select-none transition-opacity duration-200 relative overflow-hidden',
                isRemoving && 'opacity-0',
                isUnavailable && 'opacity-60 grayscale'
            )}>
                <span className={cn(
                    'size-5 rounded-md text-xs font-bold grid place-items-center shrink-0',
                    isUnavailable ? 'bg-muted text-muted-foreground' : 'bg-primary/10 text-primary'
                )}>
                    {index + 1}
                </span>

                <div
                    className={cn(
                        'p-0.5 rounded touch-none transition-colors',
                        isUnavailable
                            ? 'cursor-grab active:cursor-grabbing hover:bg-muted/60'
                            : 'cursor-grab active:cursor-grabbing hover:bg-muted'
                    )}
                    // eslint-disable-next-line react-hooks/refs
                    {...dnd.dragHandleProps}
                    aria-label={t('member.reorder', { name: member.name })}
                >
                    <GripVertical className="size-3.5 text-muted-foreground" />
                </div>

                <span className={cn(isUnavailable && 'opacity-70')}>
                    <ModelAvatar size={18} />
                </span>

                <div className="flex flex-col min-w-0 flex-1">
                    <div className="flex min-w-0 items-center gap-1.5">
                        <Tooltip side="top" sideOffset={10} align="start">
                            <TooltipTrigger className={cn(
                                'block min-w-0 flex-1 truncate text-left text-sm font-medium leading-tight',
                                isUnavailable && 'text-muted-foreground'
                            )}>
                                {member.name}
                            </TooltipTrigger>
                            <TooltipContent key={member.name}>{member.name}</TooltipContent>
                        </Tooltip>
                        <CapabilityBadges capabilities={member.capabilities} size="xs" />
                        {member.is_reserve && (
                            <span className="ml-auto inline-flex shrink-0 rounded-full border border-amber-500/30 bg-amber-500/10 px-1.5 py-px text-[9px] font-medium text-amber-700 dark:text-amber-300">
                                {t('member.reserveBadge')}
                            </span>
                        )}
                        {isDisabled && (
                            <Badge variant="outline" className="shrink-0 px-1.5 py-px text-[9px] bg-muted text-muted-foreground">
                                {t('member.disabledBadge')}
                            </Badge>
                        )}
                        {isBalanceInsufficient && (
                            <Badge variant="outline" className="shrink-0 px-1.5 py-px text-[9px] bg-destructive/10 text-destructive">
                                {t('member.balanceInsufficientBadge')}
                            </Badge>
                        )}
                        {member.policy_status === 'blocked' && (
                            <Badge variant="destructive" className="ml-auto shrink-0 px-1.5 py-px text-[9px]" title={member.policy_reason || undefined}>
                                倍率阻断
                            </Badge>
                        )}
                        {member.supports_tools === true && (
                            <span
                                title={`${t('tools.badgeToolsYes')}${member.supports_tools_source ? ` · ${t('tools.source')} ${member.supports_tools_source}` : ''}`}
                                className="inline-flex shrink-0 items-center rounded px-1 py-px text-[9px] font-medium leading-none bg-emerald-500/15 text-emerald-700 dark:text-emerald-300"
                            >
                                ✓tools
                            </span>
                        )}
                        {member.supports_tools === false && (
                            <span
                                title={`${t('tools.badgeToolsNo')}${member.supports_tools_source ? ` · ${t('tools.source')} ${member.supports_tools_source}` : ''}`}
                                className="inline-flex shrink-0 items-center rounded px-1 py-px text-[9px] font-medium leading-none bg-destructive/10 text-destructive"
                            >
                                ✗tools
                            </span>
                        )}
                        {/* supports_tools 为 null/undefined（未探测）不渲染，保持布局精简 */}
                        {(onToolsTest || onToolsForce || onToolsReset) && (
                            <span className="ml-auto inline-flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 pointer-events-none group-hover:pointer-events-auto focus-within:pointer-events-auto pointer-coarse:opacity-100 pointer-coarse:pointer-events-auto">
                                {onToolsTest && (
                                    <button
                                        type="button"
                                        onClick={() => onToolsTest(member)}
                                        title={t('tools.batchTestTitle')}
                                        aria-label={t('tools.batchTestTitle')}
                                        disabled={isUnavailable || toolsDisabled}
                                        className="rounded p-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-40 disabled:pointer-events-none"
                                    >
                                        <FlaskConical className="size-3" />
                                    </button>
                                )}
                                {onToolsForce && canForce && (
                                    <button
                                        type="button"
                                        onClick={() => onToolsForce(member)}
                                        title={t('tools.forceUnsupported')}
                                        aria-label={t('tools.forceUnsupported')}
                                        disabled={isUnavailable || toolsDisabled}
                                        className="rounded p-0.5 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-40 disabled:pointer-events-none"
                                    >
                                        <Ban className="size-3" />
                                    </button>
                                )}
                                {onToolsReset && canReset && (
                                    <button
                                        type="button"
                                        onClick={() => onToolsReset(member)}
                                        title={t('tools.resetTools')}
                                        aria-label={t('tools.resetTools')}
                                        disabled={isUnavailable || toolsDisabled}
                                        className="rounded p-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-40 disabled:pointer-events-none"
                                    >
                                        <RotateCcw className="size-3" />
                                    </button>
                                )}
                            </span>
                        )}
                    </div>
                    <span className="text-[10px] text-muted-foreground truncate leading-tight">{sourceLabel}</span>
                    {(member.balance != null || (member.site_id != null && (member.group_multiplier != null || member.multiplier_known !== true)) || member.supports_tools != null) && (
                        <span className="flex items-center gap-1 mt-0.5">
                            {member.balance != null && (
                                <Badge variant="secondary" className="shrink-0 gap-1 px-1.5 py-0 text-[9px] bg-emerald-500/15 text-emerald-700 dark:text-emerald-300">
                                    <Wallet className="size-2.5 shrink-0" />
                                    {formatBalance(member.balance)}
                                </Badge>
                            )}
                            {member.multiplier_known === true && member.group_multiplier != null && (
                                <Badge variant="secondary" className="shrink-0 gap-1 px-1.5 py-0 text-[9px] bg-amber-500/15 text-amber-700 dark:text-amber-300">
                                    <Coins className="size-2.5 shrink-0" />
                                    {formatMultiplier(member.group_multiplier)}
                                </Badge>
                            )}
                            {member.multiplier_known !== true && member.site_id != null && member.group_multiplier != null && (
                                <Badge
                                    variant="secondary"
                                    className="shrink-0 gap-1 px-1.5 py-0 text-[9px] bg-sky-500/15 text-sky-700 dark:text-sky-300"
                                    title={`保留站点旧值 ${formatMultiplier(member.group_multiplier)}，实际计费以站点报价为准`}
                                >
                                    <Coins className="size-2.5 shrink-0" />
                                    暂定 {formatMultiplier(member.group_multiplier)}
                                </Badge>
                            )}
                            {member.multiplier_known !== true && member.site_id != null && member.group_multiplier == null && (
                                <Badge
                                    variant="secondary"
                                    className="shrink-0 gap-1 px-1.5 py-0 text-[9px] bg-sky-500/15 text-sky-700 dark:text-sky-300"
                                    title="站点未再上报倍率，当前按 1x 处理"
                                >
                                    <Coins className="size-2.5 shrink-0" />
                                    暂定 1x
                                </Badge>
                            )}
                        </span>
                    )}
                </div>

                {showWeight && (
                    <input
                        type="number"
                        min={1}
                        value={member.weight ?? 1}
                        onChange={(e) => onWeightChange?.(member.id, Math.max(1, parseInt(e.target.value) || 1))}
                        className={cn(
                            'w-12 h-6 text-xs text-center rounded border border-border bg-muted/50 focus:outline-none focus:ring-1 focus:ring-primary',
                            isDisabled && 'text-muted-foreground'
                        )}
                    />
                )}

                {(!showConfirmDelete || !confirmDelete) && (
                    <motion.button
                        layoutId={`delete-btn-member-${layoutScope ?? 'default'}-${member.id}`}
                        type="button"
                        onClick={() => showConfirmDelete ? setConfirmDelete(true) : onRemove(member.id)}
                        aria-label={t('member.remove', { name: member.name })}
                        className="p-1 rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
                        initial={false}
                        animate={{ opacity: 1, x: 0 }}
                        transition={{ duration: 0.15 }}
                        style={{ pointerEvents: 'auto' }}
                    >
                        <X className="size-3" />
                    </motion.button>
                )}

                <AnimatePresence>
                    {showConfirmDelete && confirmDelete && (
                        <motion.div
                            layoutId={`delete-btn-member-${layoutScope ?? 'default'}-${member.id}`}
                            className="absolute inset-0 flex items-center justify-center gap-2 bg-destructive p-1.5 rounded-lg"
                            transition={{ type: 'spring', stiffness: 400, damping: 30 }}
                        >
                            <button
                                type="button"
                                onClick={() => setConfirmDelete(false)}
                                aria-label={t('member.cancelRemove', { name: member.name })}
                                className="flex h-6 w-6 items-center justify-center rounded-md bg-destructive-foreground/20 text-destructive-foreground transition-all hover:bg-destructive-foreground/30 active:scale-95"
                            >
                                <X className="h-3 w-3" />
                            </button>
                            <button
                                type="button"
                                onClick={() => onRemove(member.id)}
                                aria-label={t('member.confirmRemove', { name: member.name })}
                                className="flex-1 h-6 flex items-center justify-center gap-1.5 rounded-md bg-destructive-foreground text-destructive text-xs font-semibold transition-all hover:bg-destructive-foreground/90 active:scale-[0.98]"
                            >
                                <Trash2 className="h-3 w-3" />
                            </button>
                        </motion.div>
                    )}
                </AnimatePresence>
            </div>
        </div>
    );
}

export interface MemberListProps {
    members: SelectedMember[];
    onReorder: (members: SelectedMember[]) => void;
    onRemove: (id: string) => void;
    onWeightChange?: (id: string, weight: number) => void;
    /**
     * When true, auto-scroll the list to bottom when a *new visible* member appears
     * (i.e. a new member id is added). Useful in "editor" flows. Defaults to true.
     */
    autoScrollOnAdd?: boolean;
    onDragStart?: () => void;
    /**
     * Called only when a drop results in a different order (i.e. commit reorder).
     * Useful for persisting the new order.
     */
    onDrop?: (members: SelectedMember[]) => void;
    /**
     * Called whenever a drag ends (including cancel / same-index drop).
     * Useful for lifecycle cleanup (e.g. clearing "isDragging" flags).
     */
    onDragFinish?: () => void;
    removingIds?: Set<string>;
    showWeight?: boolean;
    /**
     * When true, show a confirmation overlay before removing an item.
     * When false, clicking the delete button removes the item immediately.
     * Defaults to true.
     */
    showConfirmDelete?: boolean;
    layoutScope?: string;
    /** tools 操作回调（v3.1）：手动测试 / 强制标不支持 / 恢复自动。缺省不渲染操作按钮。 */
    onToolsTest?: (member: SelectedMember) => void;
    onToolsForce?: (member: SelectedMember) => void;
    onToolsReset?: (member: SelectedMember) => void;
    /** 行级 tools 按钮禁用（批量进行中 / 预设编辑器不可用，前端对抗者 P2-1/P2-7）。 */
    toolsDisabled?: boolean;
}

export function MemberList({
    members,
    onReorder,
    onRemove,
    onWeightChange,
    autoScrollOnAdd = true,
    onDragStart,
    onDrop,
    onDragFinish,
    removingIds = new Set(),
    showWeight = false,
    showConfirmDelete = true,
    layoutScope: externalLayoutScope,
    onToolsTest,
    onToolsForce,
    onToolsReset,
    toolsDisabled = false,
}: MemberListProps) {
    const internalLayoutScope = useId();
    const layoutScope = externalLayoutScope ?? internalLayoutScope;

    const scrollContainerRef = useRef<HTMLDivElement | null>(null);
    const prevMemberCountRef = useRef<number>(0);
    const hasMountedRef = useRef(false);

    const visibleCount = members.filter((m) => !removingIds.has(m.id)).length;
    const isEmpty = visibleCount === 0;
    const t = useTranslations('group');

    useEffect(() => {
        // Skip the initial mount so we don't auto-scroll on first render / initial data load.
        if (!hasMountedRef.current) {
            hasMountedRef.current = true;
            prevMemberCountRef.current = members.length;
            return;
        }

        if (!autoScrollOnAdd) {
            prevMemberCountRef.current = members.length;
            return;
        }

        const hasNewMember = members.length > prevMemberCountRef.current;

        // Auto-scroll only when member count increases (i.e. added; not reorder / not "unhide").
        if (hasNewMember) {
            // Wait a tick for DOM/placeholder/layout to settle.
            requestAnimationFrame(() => {
                const el = scrollContainerRef.current;
                if (!el) return;
                el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
            });
        }

        prevMemberCountRef.current = members.length;
    }, [members.length, autoScrollOnAdd]);

    const handleDragEnd = (result: DropResult) => {
        try {
            const { destination, source } = result;
            if (!destination) return;
            if (destination.index === source.index) return;

            const next = reorderList(members, source.index, destination.index);
            onReorder(next);
            onDrop?.(next);
        } finally {
            // Ensure drag lifecycle always finishes, even when drop is canceled.
            onDragFinish?.();
        }
    };

    return (
        <div className="relative h-full min-h-0">
            <div
                className={cn(
                    'absolute inset-0 flex flex-col items-center justify-center gap-2 text-muted-foreground',
                    'transition-opacity duration-200 ease-out',
                    isEmpty ? 'opacity-100' : 'opacity-0 pointer-events-none'
                )}
            >
                <Layers className="size-10 opacity-40" />
                <span className="text-sm">{t('card.empty')}</span>
            </div>

            <div
                className={cn(
                    'h-full overflow-y-auto transition-opacity duration-200',
                    isEmpty ? 'opacity-0' : 'opacity-100'
                )}
                ref={scrollContainerRef}
            >
                <DragDropContext
                    onDragStart={() => onDragStart?.()}
                    onDragEnd={handleDragEnd}
                >
                    <Droppable droppableId={`members-${layoutScope}`}>
                        {(droppableProvided) => (
                            <div
                                ref={droppableProvided.innerRef}
                                {...droppableProvided.droppableProps}
                                className="p-2 flex flex-col space-y-1.5"
                            >
                                {members.map((member, index) => (
                                    <Draggable
                                        key={member.id}
                                        draggableId={member.id}
                                        index={index}
                                        isDragDisabled={removingIds.has(member.id)}
                                    >
                                        {(draggableProvided, snapshot) => (
                                            <MemberItem
                                                member={member}
                                                onRemove={onRemove}
                                                onWeightChange={onWeightChange}
                                                isRemoving={removingIds.has(member.id)}
                                                index={index}
                                                showWeight={showWeight}
                                                showConfirmDelete={showConfirmDelete}
                                                layoutScope={layoutScope}
                                                onToolsTest={onToolsTest}
                                                onToolsForce={onToolsForce}
                                                onToolsReset={onToolsReset}
                                                toolsDisabled={toolsDisabled}
                                                dnd={{
                                                    innerRef: draggableProvided.innerRef,
                                                    draggableProps: draggableProvided.draggableProps,
                                                    dragHandleProps: draggableProvided.dragHandleProps,
                                                    isDragging: snapshot.isDragging,
                                                }}
                                            />
                                        )}
                                    </Draggable>
                                ))}
                                {droppableProvided.placeholder}
                            </div>
                        )}
                    </Droppable>
                </DragDropContext>
            </div>
        </div>
    );
}
