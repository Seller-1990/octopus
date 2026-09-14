"use client";

import { Badge } from '@/components/ui/badge';
import { Bar, CartesianGrid, ComposedChart, Legend, Line, ResponsiveContainer, Tooltip as RechartsTooltip, XAxis, YAxis } from 'recharts';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/zh-cn';
import 'dayjs/locale/zh-tw';

dayjs.extend(relativeTime);
import { formatHistoryTime, routeTypeLabel, summarizeHistory } from './utils';
import type { SiteModelView } from './utils';

export function HistorySummary({ model }: { model: SiteModelView }) {
    const summary = summarizeHistory(model.history);
    const buckets = model.history?.buckets ?? [];
    const bucketSpan = model.history?.bucket_span ?? 0;

    const chartData = buckets.map((b) => {
        const total = b.success + b.failure;
        const successRate = total === 0 ? null : Math.round((b.success / total) * 100);
        return {
            time: b.time,
            success: b.success,
            failure: b.failure,
            total,
            successRate,
        };
    });

    const formatBucketTime = (time: number) => {
        const d = dayjs(time * 1000);
        if (bucketSpan >= 7 * 86400) return d.format('YY-MM-DD');
        if (bucketSpan >= 86400) return d.format('MM-DD');
        if (bucketSpan >= 3600) return d.format('MM-DD HH:mm');
        return d.format('HH:mm');
    };

    return (
        <div className="w-[24rem] space-y-3 p-4 text-left">
            <div className="space-y-1">
                <div className="flex items-center justify-between gap-2">
                    <div className="truncate text-sm font-semibold text-foreground">{model.model_name}</div>
                    <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                        {routeTypeLabel(model.route_type)}
                    </Badge>
                </div>
                <div className="text-[11px] text-muted-foreground">
                    {model.group_name || model.group_key} · 最近请求 {formatHistoryTime(model.history?.last_request_at ?? null)}
                </div>
            </div>

            <div className="grid grid-cols-3 gap-2 text-xs">
                <div className="rounded-xl border border-border/60 bg-background/70 px-2 py-2">
                    <div className="text-muted-foreground">成功</div>
                    <div className="mt-1 font-semibold text-emerald-600 dark:text-emerald-300">{summary.successCount}</div>
                </div>
                <div className="rounded-xl border border-border/60 bg-background/70 px-2 py-2">
                    <div className="text-muted-foreground">失败</div>
                    <div className="mt-1 font-semibold text-destructive">{summary.failureCount}</div>
                </div>
                <div className="rounded-xl border border-border/60 bg-background/70 px-2 py-2">
                    <div className="text-muted-foreground">成功率</div>
                    <div className="mt-1 font-semibold text-foreground">{(summary.successRate * 100).toFixed(0)}%</div>
                </div>
            </div>

            {chartData.length > 0 ? (
                <div className="rounded-xl border border-border/60 bg-background/70 p-2">
                    <div className="h-36 w-full">
                        <ResponsiveContainer width="100%" height="100%">
                            <ComposedChart data={chartData} margin={{ top: 8, right: 8, bottom: 4, left: 4 }}>
                                <defs>
                                    <linearGradient id="fillSiteChannelBar" x1="0" y1="0" x2="0" y2="1">
                                        <stop offset="5%" stopColor="var(--chart-2)" stopOpacity={0.55} />
                                        <stop offset="95%" stopColor="var(--chart-2)" stopOpacity={0.15} />
                                    </linearGradient>
                                </defs>
                                <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border/50" />
                                <XAxis
                                    dataKey="time"
                                    tickFormatter={formatBucketTime}
                                    tick={{ fontSize: 10 }}
                                    tickLine={false}
                                    axisLine={false}
                                    minTickGap={24}
                                />
                                <YAxis
                                    yAxisId="rate"
                                    domain={[0, 100]}
                                    tick={{ fontSize: 10 }}
                                    tickFormatter={(v) => `${v}%`}
                                    tickLine={false}
                                    axisLine={false}
                                    width={32}
                                />
                                <YAxis
                                    yAxisId="count"
                                    orientation="right"
                                    tick={{ fontSize: 10 }}
                                    tickLine={false}
                                    axisLine={false}
                                    width={24}
                                    allowDecimals={false}
                                />
                                <Legend
                                    iconType="circle"
                                    iconSize={7}
                                    height={14}
                                    wrapperStyle={{ fontSize: 10, paddingTop: 2, lineHeight: '14px' }}
                                />
                                <RechartsTooltip
                                    cursor={{ fill: 'var(--muted)', fillOpacity: 0.4 }}
                                    content={({ active, payload }: { active?: boolean; payload?: Array<{ payload?: unknown }> }) => {
                                        if (!active || !payload || payload.length === 0) return null;
                                        const point = payload[0].payload as typeof chartData[number];
                                        return (
                                            <div className="rounded-lg border border-border/70 bg-popover/95 px-3 py-2 text-[11px] shadow-md backdrop-blur">
                                                <div className="font-medium text-foreground">{formatBucketTime(point.time)}</div>
                                                <div className="mt-1 flex items-center gap-3 text-muted-foreground">
                                                    <span className="text-emerald-600 dark:text-emerald-300">成功 {point.success}</span>
                                                    <span className="text-destructive">失败 {point.failure}</span>
                                                </div>
                                                <div className="text-muted-foreground">
                                                    成功率 {point.successRate === null ? '—' : `${point.successRate}%`}
                                                </div>
                                            </div>
                                        );
                                    }}
                                />
                                <Bar
                                    yAxisId="count"
                                    dataKey="total"
                                    name="请求数"
                                    fill="url(#fillSiteChannelBar)"
                                    radius={[2, 2, 0, 0]}
                                    isAnimationActive={false}
                                />
                                <Line
                                    yAxisId="rate"
                                    type="monotone"
                                    dataKey="successRate"
                                    name="成功率"
                                    stroke="var(--chart-1)"
                                    strokeWidth={2}
                                    dot={{ r: 2, fill: 'var(--chart-1)', stroke: 'var(--chart-1)' }}
                                    activeDot={{ r: 3 }}
                                    connectNulls
                                    isAnimationActive={false}
                                />
                            </ComposedChart>
                        </ResponsiveContainer>
                    </div>
                </div>
            ) : (
                <div className="rounded-xl border border-dashed border-border/70 bg-background/50 px-3 py-4 text-center text-xs text-muted-foreground">
                    暂无请求历史
                </div>
            )}
        </div>
    );
}


