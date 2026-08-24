import { useTranslations } from "next-intl";
import {
  Site as SiteRecord,
  SiteAccount,
} from "@/api/endpoints/site";
import { useSettingStore } from "@/stores/setting";
import { translateSiteMessage } from "./site-message";
import { deriveCheckinStatus } from "./checkin-status";

export type HealthTone = "default" | "danger" | "muted" | "warning";

export function formatDateTime(value?: string | null) {
  if (!value) return "从未执行";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) {
    return "从未执行";
  }
  return date.toLocaleString();
}

export function statusLabel(status: string) {
  switch (status) {
    case "partial":
      return "部分成功";
    case "success":
      return "成功";
    case "failed":
      return "失败";
    case "skipped":
      return "跳过";
    case "idle":
    default:
      return "未执行";
  }
}

export function getErrorMessage(error: unknown) {
  if (error instanceof Error) return error.message;
  if (typeof error === "object" && error !== null && "message" in error) {
    const message = (error as { message?: unknown }).message;
    if (typeof message === "string") return message;
  }
  return "操作失败";
}

export function getSiteErrorMessage(
  locale: ReturnType<typeof useSettingStore.getState>["locale"],
  error: unknown,
  t?: ReturnType<typeof useTranslations>,
) {
  return translateSiteMessage(locale, getErrorMessage(error), t);
}

export function formatBalance(value: number) {
  if (value === 0) return "0";
  if (value >= 1000000) return `${(value / 1000000).toFixed(2)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(2)}K`;
  return value.toFixed(2);
}

export function normalizeSearchTerm(value: string) {
  return value.trim().toLowerCase();
}

export function matchesSearch(value: string | null | undefined, query: string) {
  return (value ?? "").toLowerCase().includes(query);
}

export function normalizedStatus(status?: string | null) {
  return status || "idle";
}

export function accountHasSyncFailure(account: SiteAccount) {
  return normalizedStatus(account.last_sync_status) === "failed";
}

export function accountHasCheckinFailure(
  site: SiteRecord,
  account: SiteAccount,
) {
  return deriveCheckinStatus(site, account) === "failed";
}

export function accountHasHealthFailure(
  site: SiteRecord,
  account: SiteAccount,
) {
  return accountHasSyncFailure(account) || accountHasCheckinFailure(site, account);
}

export function statusDotClass(status: string) {
  switch (status) {
    case "success":
      return "bg-emerald-500";
    case "partial":
      return "bg-amber-500";
    case "failed":
      return "bg-destructive";
    case "skipped":
      return "bg-amber-500";
    default:
      return "bg-muted-foreground/40";
  }
}

export function badgeToneClass(tone: HealthTone) {
  switch (tone) {
    case "danger":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "muted":
      return "border-border bg-muted/40 text-muted-foreground";
    case "warning":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "default":
    default:
      return "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
  }
}

export function cardToneClass(tone: HealthTone) {
  switch (tone) {
    case "danger":
      return "border-destructive/25 bg-gradient-to-br from-destructive/[0.07] via-card to-card";
    case "muted":
      return "border-slate-400/25 bg-gradient-to-br from-slate-500/[0.06] via-card to-card dark:border-slate-600/35";
    case "warning":
      return "border-amber-500/25 bg-gradient-to-br from-amber-500/[0.07] via-card to-card";
    case "default":
    default:
      return "border-border/70 bg-card";
  }
}

export function isCloudflareProtectionMessage(message?: string | null) {
  return (message ?? "").toLowerCase().includes("cloudflare");
}
