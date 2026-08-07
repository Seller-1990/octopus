"use client";

import { useCallback, useMemo, useState, type ReactNode } from "react";
import { useTranslations } from "next-intl";
import {
  AlertTriangle,
  CalendarCheck2,
  ChevronDown,
  ChevronUp,
  ExternalLink,
  FilterX,
  Layers3,
  Tag,
  TrendingUp,
  Wallet,
} from "lucide-react";
import { type Site } from "@/api/endpoints/site";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  buildCheckinSummary,
  type CheckinActiveFilterStatus,
  type CheckinFilterStatus,
} from "./checkin-status";
import type { BatchFailureCategory } from "./batch-failure";

const FAILURE_FILTERS: Array<{
  key: BatchFailureCategory;
  labelKey:
    | "filters.failureCredential"
    | "filters.failureRisk"
    | "filters.failureTransient"
    | "filters.failureOther";
}> = [
  { key: "credential", labelKey: "filters.failureCredential" },
  { key: "risk", labelKey: "filters.failureRisk" },
  { key: "transient", labelKey: "filters.failureTransient" },
  { key: "other", labelKey: "filters.failureOther" },
];

function filterTone(status: CheckinFilterStatus, active: boolean) {
  if (active) {
    switch (status) {
      case "success":
        return "border-emerald-500/30 bg-emerald-500 text-white";
      case "partial":
        return "border-amber-500/30 bg-amber-500 text-white";
      case "failed":
        return "border-destructive/30 bg-destructive text-white";
      case "idle":
        return "border-border bg-foreground text-background";
      case "disabled":
        return "border-slate-500/30 bg-slate-700 text-white dark:bg-slate-200 dark:text-slate-900";
      case "reserve":
        return "border-amber-500/30 bg-amber-500 text-white";
      case "sync_failed":
        return "border-destructive/30 bg-destructive text-white";
      case "all":
      default:
        return "border-primary/30 bg-primary text-primary-foreground";
    }
  }

  switch (status) {
    case "success":
      return "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "partial":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "failed":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "idle":
      return "border-border bg-muted/40 text-muted-foreground";
    case "disabled":
      return "border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-300";
    case "reserve":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "sync_failed":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "all":
    default:
      return "border-border bg-background text-foreground";
  }
}

function failureFilterTone(category: BatchFailureCategory, active: boolean) {
  if (active) {
    switch (category) {
      case "credential":
        return "border-red-500/30 bg-red-500 text-white";
      case "risk":
        return "border-orange-500/30 bg-orange-500 text-white";
      case "transient":
        return "border-amber-500/30 bg-amber-500 text-white";
      case "other":
        return "border-slate-500/30 bg-slate-700 text-white dark:bg-slate-200 dark:text-slate-900";
    }
  }

  switch (category) {
    case "credential":
      return "border-red-500/20 bg-red-500/10 text-red-700 dark:text-red-300";
    case "risk":
      return "border-orange-500/20 bg-orange-500/10 text-orange-700 dark:text-orange-300";
    case "transient":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "other":
      return "border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-300";
  }
}

function FilterChip({
  label,
  active,
  onClick,
  hasExpand,
  expanded,
  tone,
}: {
  label: string;
  active: boolean;
  onClick?: () => void;
  hasExpand?: boolean;
  expanded?: boolean;
  tone?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium transition-colors",
        tone
          ? tone
          : active
            ? "bg-primary text-primary-foreground"
            : "bg-muted text-muted-foreground hover:bg-muted/80",
      )}
    >
      {label}
      {hasExpand &&
        (expanded ? (
          <ChevronUp className="size-3" />
        ) : (
          <ChevronDown className="size-3" />
        ))}
    </button>
  );
}

function formatCurrency(value: number) {
  const safe = Number.isFinite(value) ? value : 0;
  return `$${safe.toFixed(2)}`;
}

function OverviewMetric({
  icon,
  label,
  value,
  tone,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  tone?: "default" | "warning";
}) {
  return (
    <div className="flex items-center gap-3 rounded-2xl bg-muted/20 px-4 py-3">
      <span
        className={cn(
          "flex size-9 items-center justify-center rounded-xl bg-background shadow-sm",
          tone === "warning"
            ? "text-amber-600 dark:text-amber-400"
            : "text-muted-foreground",
        )}
      >
        {icon}
      </span>
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-base font-semibold truncate">{value}</div>
      </div>
    </div>
  );
}

export function CheckinPanel({
  sites,
  inventory,
  statusDayKey,
  visibleSiteCount,
  visibleAccountCount,
  searchTerm,
  hasActiveFilters,
  onClearFilters,
  activeFilterStatuses,
  onFilterChange,
  failureFilterCounts,
  activeFailureFilters,
  onFailureFilterChange,
  syncFailureFilterCounts,
  activeSyncFailureFilters,
  onSyncFailureFilterChange,
  allTags,
  activeTags,
  onTagFilterChange,
}: {
  sites: Site[] | undefined;
  inventory: {
    totalBalance: number;
    totalBalanceUsed: number;
    enabledAccounts: number;
    totalAccounts: number;
  };
  statusDayKey: string;
  visibleSiteCount: number;
  visibleAccountCount: number;
  searchTerm: string;
  hasActiveFilters: boolean;
  onClearFilters: () => void;
  activeFilterStatuses: CheckinActiveFilterStatus[];
  onFilterChange: (status: CheckinFilterStatus) => void;
  failureFilterCounts: Record<BatchFailureCategory, number>;
  activeFailureFilters: BatchFailureCategory[];
  onFailureFilterChange: (category: BatchFailureCategory) => void;
  syncFailureFilterCounts: Record<BatchFailureCategory, number>;
  activeSyncFailureFilters: BatchFailureCategory[];
  onSyncFailureFilterChange: (category: BatchFailureCategory) => void;
  allTags: Array<{ tag: string; count: number }>;
  activeTags: string[];
  onTagFilterChange: (tag: string) => void;
}) {
  const t = useTranslations("siteManagement.checkin");
  const summaryNow = useMemo(() => {
    const [year = "", month = "", day = ""] = statusDayKey.split("-");
    const parsed = new Date(Number(year), Number(month), Number(day));
    return Number.isNaN(parsed.getTime()) ? new Date() : parsed;
  }, [statusDayKey]);

  const summary = useMemo(
    () => buildCheckinSummary(sites, summaryNow),
    [sites, summaryNow],
  );

  const enabledCount = useMemo(
    () => (sites ?? []).filter((s) => s.enabled).length,
    [sites],
  );
  const disabledCount = useMemo(
    () => (sites ?? []).filter((s) => !s.enabled).length,
    [sites],
  );
  const reserveCount = useMemo(
    () => (sites ?? []).filter((s) => s.is_reserve).length,
    [sites],
  );
  const syncFailedCount = useMemo(
    () => (sites ?? []).filter((s) =>
      s.accounts?.some((a) => a.last_sync_status === 'failed'),
    ).length,
    [sites],
  );

  const [checkinFailedExpanded, setCheckinFailedExpanded] = useState(false);
  const [syncFailedExpanded, setSyncFailedExpanded] = useState(false);

  const hasContextBadges = Boolean(searchTerm);

  const manualCheckinUrls = useMemo(
    () =>
      (sites ?? [])
        .filter((s) => s.external_checkin_url?.trim())
        .map((s) => s.external_checkin_url!.trim()),
    [sites],
  );

  const openAllManualCheckin = useCallback(() => {
    for (const url of manualCheckinUrls) {
      window.open(url, "_blank", "noopener,noreferrer");
    }
  }, [manualCheckinUrls]);

  const isSyncFailedActive = activeFilterStatuses.includes("sync_failed") || activeSyncFailureFilters.length > 0;
  const isCheckinFailedActive = activeFilterStatuses.includes("failed") || activeFailureFilters.length > 0;

  function handleSyncFailedToggle() {
    if (isSyncFailedActive) {
      if (activeFilterStatuses.includes("sync_failed")) {
        onFilterChange("sync_failed");
      }
      for (const f of activeSyncFailureFilters) {
        onSyncFailureFilterChange(f);
      }
      setSyncFailedExpanded(false);
    } else {
      onFilterChange("sync_failed");
      setSyncFailedExpanded(true);
    }
  }

  function handleCheckinFailedToggle() {
    if (isCheckinFailedActive) {
      if (activeFilterStatuses.includes("failed")) {
        onFilterChange("failed");
      }
      for (const f of activeFailureFilters) {
        onFailureFilterChange(f);
      }
      setCheckinFailedExpanded(false);
    } else {
      onFilterChange("failed");
      setCheckinFailedExpanded(true);
    }
  }

  return (
    <section className="overflow-hidden rounded-[28px] border border-border/70 bg-card shadow-[0_18px_60px_-40px_rgba(15,23,42,0.45)]">
      <div className="border-b border-border/60 bg-gradient-to-br from-background via-card to-muted/10 px-5 py-5">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex flex-wrap items-center gap-2 text-base font-semibold">
            <CalendarCheck2 className="size-5 text-primary" />
            <span>{t("overview")}</span>
            <span className="text-xs font-normal text-muted-foreground">
              {t("totalStats", {
                siteCount: sites?.length ?? 0,
                accountCount: summary.total,
              })}
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>{t("currentResult")}</span>
            <span className="font-medium text-foreground">
              {t("visibleCounts", {
                siteCount: visibleSiteCount,
                accountCount: visibleAccountCount,
              })}
            </span>
          </div>
        </div>

        <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <OverviewMetric
            icon={<Wallet className="size-4" />}
            label={t("currentBalance")}
            value={formatCurrency(inventory.totalBalance)}
          />
          <OverviewMetric
            icon={<TrendingUp className="size-4" />}
            label={t("totalUsed")}
            value={formatCurrency(inventory.totalBalanceUsed)}
          />
          <OverviewMetric
            icon={<Layers3 className="size-4" />}
            label={t("enabledAccounts")}
            value={`${inventory.enabledAccounts} / ${inventory.totalAccounts}`}
          />
          <OverviewMetric
            icon={<AlertTriangle className="size-4" />}
            label={t("todayFailures")}
            value={`${summary.failed}`}
            tone={summary.failed > 0 ? "warning" : "default"}
          />
        </div>

        {hasActiveFilters && hasContextBadges ? (
          <div className="mt-4 flex flex-wrap gap-2">
            {searchTerm ? (
              <Badge variant="outline">{t("search", { term: searchTerm })}</Badge>
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="px-5 py-3">
        <div className="flex flex-wrap items-center gap-1.5">
          {/* Site dimension */}
          <FilterChip
            label={`站点 ${sites?.length ?? 0}`}
            active={
              activeFilterStatuses.length === 0 &&
              activeFailureFilters.length === 0 &&
              activeSyncFailureFilters.length === 0
            }
            onClick={() => onFilterChange("all")}
          />
          <FilterChip
            label={`启用 ${enabledCount}`}
            active={false}
          />
          <FilterChip
            label={`禁用 ${disabledCount}`}
            active={activeFilterStatuses.includes("disabled")}
            onClick={() => onFilterChange("disabled")}
            tone={filterTone("disabled", activeFilterStatuses.includes("disabled"))}
          />
          <FilterChip
            label={`中转 ${reserveCount}`}
            active={activeFilterStatuses.includes("reserve")}
            onClick={() => onFilterChange("reserve")}
            tone={filterTone("reserve", activeFilterStatuses.includes("reserve"))}
          />
          {syncFailedCount > 0 && (
            <FilterChip
              label={`同步失败 ${syncFailedCount}`}
              active={isSyncFailedActive}
              onClick={handleSyncFailedToggle}
              tone={filterTone("sync_failed", isSyncFailedActive)}
              hasExpand
              expanded={syncFailedExpanded}
            />
          )}

          <span className="mx-1 h-4 w-px bg-border" />

          {/* Checkin dimension */}
          <FilterChip
            label={`签到 ${summary.total}`}
            active={
              activeFilterStatuses.length === 0 &&
              activeFailureFilters.length === 0 &&
              activeSyncFailureFilters.length === 0
            }
            onClick={() => onFilterChange("all")}
          />
          <FilterChip
            label={`成功 ${summary.success}`}
            active={activeFilterStatuses.includes("success")}
            onClick={() => onFilterChange("success")}
            tone={filterTone("success", activeFilterStatuses.includes("success"))}
          />
          <FilterChip
            label={`部分 ${summary.partial}`}
            active={activeFilterStatuses.includes("partial")}
            onClick={() => onFilterChange("partial")}
            tone={filterTone("partial", activeFilterStatuses.includes("partial"))}
          />
          <FilterChip
            label={`失败 ${summary.failed}`}
            active={isCheckinFailedActive}
            onClick={handleCheckinFailedToggle}
            tone={filterTone("failed", isCheckinFailedActive)}
            hasExpand={summary.failed > 0}
            expanded={checkinFailedExpanded}
          />

          {/* Actions */}
          <span className="ml-auto" />
          {hasActiveFilters ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="rounded-xl text-xs"
              onClick={onClearFilters}
            >
              <FilterX className="size-4" />
              {t("clearFilters")}
            </Button>
          ) : null}
          {manualCheckinUrls.length > 0 ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="rounded-xl text-xs"
              onClick={openAllManualCheckin}
            >
              <ExternalLink className="size-4" />
              {t("openManualCheckin", { count: manualCheckinUrls.length })}
            </Button>
          ) : null}
        </div>

        {/* Expandable sync failure details */}
        {syncFailedExpanded && syncFailedCount > 0 && (
          <div className="mt-2 flex flex-wrap gap-1.5">
            <FilterChip
              label={`全部同步失败 ${syncFailedCount}`}
              active={activeFilterStatuses.includes("sync_failed") && activeSyncFailureFilters.length === 0}
              onClick={() => {
                if (!activeFilterStatuses.includes("sync_failed")) {
                  onFilterChange("sync_failed");
                }
                if (activeSyncFailureFilters.length > 0) {
                  for (const f of activeSyncFailureFilters) {
                    onSyncFailureFilterChange(f);
                  }
                }
              }}
              tone={filterTone("failed", activeFilterStatuses.includes("sync_failed") && activeSyncFailureFilters.length === 0)}
            />
            {FAILURE_FILTERS.map((filter) => {
              const count = syncFailureFilterCounts[filter.key];
              if (count === 0) return null;
              const active = activeSyncFailureFilters.includes(filter.key);
              return (
                <FilterChip
                  key={filter.key}
                  label={`${t(filter.labelKey)} ${count}`}
                  active={active}
                  onClick={() => onSyncFailureFilterChange(filter.key)}
                  tone={failureFilterTone(filter.key, active)}
                />
              );
            })}
          </div>
        )}

        {/* Expandable checkin failure details */}
        {checkinFailedExpanded && summary.failed > 0 && (
          <div className="mt-2 flex flex-wrap gap-1.5">
            <FilterChip
              label={`全部签到失败 ${summary.failed}`}
              active={activeFilterStatuses.includes("failed") && activeFailureFilters.length === 0}
              onClick={() => {
                if (!activeFilterStatuses.includes("failed")) {
                  onFilterChange("failed");
                }
                if (activeFailureFilters.length > 0) {
                  for (const f of activeFailureFilters) {
                    onFailureFilterChange(f);
                  }
                }
              }}
              tone={filterTone("failed", activeFilterStatuses.includes("failed") && activeFailureFilters.length === 0)}
            />
            {FAILURE_FILTERS.map((filter) => {
              const count = failureFilterCounts[filter.key];
              if (count === 0) return null;
              const active = activeFailureFilters.includes(filter.key);
              return (
                <FilterChip
                  key={filter.key}
                  label={`${t(filter.labelKey)} ${count}`}
                  active={active}
                  onClick={() => onFailureFilterChange(filter.key)}
                  tone={failureFilterTone(filter.key, active)}
                />
              );
            })}
          </div>
        )}

        {allTags.length > 0 ? (
          <div className="mt-3 flex flex-wrap gap-2">
            {allTags.map(({ tag, count }) => {
              const active = activeTags.includes(tag);
              return (
                <button
                  key={tag}
                  type="button"
                  onClick={() => onTagFilterChange(tag)}
                  title={
                    active
                      ? t("removeTagFilter", { tag })
                      : t("addTagFilter", { tag })
                  }
                  className={cn(
                    "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
                    active
                      ? "border-primary/30 bg-primary text-primary-foreground"
                      : "border-border bg-background text-foreground hover:bg-muted/40",
                  )}
                >
                  <Tag className="size-3" />
                  <span>{tag}</span>
                  <span className="text-[10px] opacity-70">{count}</span>
                </button>
              );
            })}
          </div>
        ) : null}
      </div>
    </section>
  );
}
