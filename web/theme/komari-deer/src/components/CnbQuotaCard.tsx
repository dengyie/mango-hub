"use client";

import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Activity } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * CNB AI 额度卡：消费 hk /ops/quota 公开 JSON（CNB cron 流水线每 5 分钟现签令牌同步，hk 零令牌）。
 * 数据字段：credit_total_milli / credit_used_milli（milli-credit）、dev_used_sec / dev_total_sec（核秒）。
 * updated_at 停滞 >30 分钟视为同步中断（cron 正常周期 5 分钟）。
 * 由 NodeGrid 渲染在节点卡网格首位，卡面结构对齐 Node.tsx（标题行/分区线/Net 行/进度条）。
 */

const QUOTA_URL = "https://cnb-ai.mangoqwq.com/ops/quota";
const STALE_MS = 30 * 60 * 1000;

interface QuotaSnapshot {
  updated_at: string;
  credit_total_milli: number | null;
  credit_free_milli: number | null;
  credit_used_milli: number | null;
  dev_used_sec: number | null;
  ci_used_sec: number | null;
  dev_total_sec: number | null;
  dev_free_sec: number | null;
}

function formatCredits(milli: number | null | undefined): string {
  if (milli == null || !Number.isFinite(milli)) return "-";
  return (milli / 1000).toLocaleString(undefined, { maximumFractionDigits: 2 });
}

function formatCoreHours(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return "-";
  const h = seconds / 3600;
  return `${h.toLocaleString(undefined, { maximumFractionDigits: 1 })}h`;
}

export function useCnbQuota() {
  const [quota, setQuota] = useState<QuotaSnapshot | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let alive = true;
    const fetchQuota = async () => {
      try {
        const res = await fetch(QUOTA_URL, { cache: "no-store" });
        if (!res.ok) throw new Error(String(res.status));
        const data = (await res.json()) as QuotaSnapshot;
        if (alive && data && typeof data.updated_at === "string") {
          setQuota(data);
          setFailed(false);
        }
      } catch {
        if (alive) setFailed(true);
      }
    };
    void fetchQuota();
    const interval = setInterval(fetchQuota, 5 * 60 * 1000);
    return () => {
      alive = false;
      clearInterval(interval);
    };
  }, []);

  return { quota, failed };
}

function quotaPercentage(quota: QuotaSnapshot | null): number | null {
  const total = quota?.credit_total_milli ?? null;
  const used = quota?.credit_used_milli ?? null;
  if (!total || used == null) return null;
  return (used / total) * 100;
}

/** 节点网格首位卡：结构对齐 Node.tsx 卡面（标题行 → 分区线 → Credit 行 → 进度条） */
export function CnbQuotaNodeCard() {
  const [t] = useTranslation();
  const { quota, failed } = useCnbQuota();

  const stale =
    quota != null &&
    Date.now() - new Date(quota.updated_at).getTime() > STALE_MS;

  const total = quota?.credit_total_milli ?? null;
  const used = quota?.credit_used_milli ?? null;
  const percentage = quotaPercentage(quota);

  return (
    <div
      className="group relative flex min-h-[404px] w-full overflow-hidden rounded-[14px] border border-[#3a4a66]/60 bg-[linear-gradient(135deg,rgba(20,26,42,0.95)_0%,rgba(14,19,32,0.98)_50%,rgba(10,14,26,0.99)_100%)] p-4 transition-all duration-300 shadow-[inset_0_1px_0_rgba(255,255,255,0.06),0_8px_24px_rgba(0,0,0,0.4),0_2px_8px_rgba(94,109,255,0.08)] hover:border-[#5e6dff]/70 hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_12px_40px_rgba(94,109,255,0.15),0_4px_16px_rgba(94,109,255,0.12)] hover:translate-y-[-2px] select-none"
      data-cnb-quota-card
    >
      <div className="relative flex min-h-0 w-full flex-col">
        {/* 标题行：对齐节点卡（Flag 位 + 名称 + 状态灯） */}
        <div className="flex min-h-[56px] items-center justify-between">
          <div className="flex min-w-0 flex-1 items-center gap-3 overflow-hidden">
            <span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md border border-[#3a4a66]/50 bg-[#1a2535]/70 text-[11px] font-bold text-[#8a93a8]">
              AI
            </span>
            <div className="flex min-w-0 flex-1 flex-col">
              <h3 className="truncate text-base font-bold tracking-tight">
                {t("quota.title", { defaultValue: "CNB AI Quota" })}
              </h3>
              <div className="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground/80">
                <span className="whitespace-nowrap rounded-md border border-[#2a3a52]/40 bg-gradient-to-r from-[#1e2a3f]/60 to-[#1a2535]/60 px-2 py-1 font-medium backdrop-blur-sm">
                  mangoqwq-lab
                </span>
                <span
                  className={cn(
                    "h-1.5 w-1.5 flex-shrink-0 animate-pulse rounded-full shadow-[0_0_6px_currentColor]",
                    failed || stale ? "bg-[#d59a25]" : "bg-green-500"
                  )}
                  title={
                    failed
                      ? t("quota.unavailable", { defaultValue: "No data" })
                      : stale
                        ? t("quota.stale", { defaultValue: "Sync interrupted" })
                        : t("quota.synced", { defaultValue: "Synced every 5 min" })
                  }
                />
              </div>
            </div>
          </div>
        </div>

        <div className="-mx-4 mt-3 h-px bg-gradient-to-r from-transparent via-[#3a4a66]/40 to-transparent" />
        <div className="h-4" />

        {quota && total != null ? (
          <>
            <div className="grid grid-cols-3 items-start justify-items-center gap-3">
              <div className="flex flex-col items-center gap-1">
                <span className="font-mono text-lg font-extrabold text-[#d95473] tabular-nums">
                  {formatCredits(used)}
                </span>
                <span className="text-[10px] font-semibold uppercase tracking-wide text-[#9aa3b7]">
                  {t("quota.used", { defaultValue: "Used" })}
                </span>
              </div>
              <div className="flex flex-col items-center gap-1">
                <span className="font-mono text-lg font-extrabold text-[#00df7c] tabular-nums">
                  {formatCredits(total != null && used != null ? total - used : null)}
                </span>
                <span className="text-[10px] font-semibold uppercase tracking-wide text-[#9aa3b7]">
                  {t("quota.remaining", { defaultValue: "Remaining" })}
                </span>
              </div>
              <div className="flex flex-col items-center gap-1">
                <span className="font-mono text-lg font-extrabold text-[#aeb6c9] tabular-nums">
                  {formatCredits(total)}
                </span>
                <span className="text-[10px] font-semibold uppercase tracking-wide text-[#9aa3b7]">
                  {t("quota.total", { defaultValue: "Total" })}
                </span>
              </div>
            </div>

            <div className="h-4" />

            <div className="space-y-2.5 text-[13px] select-none">
              <div className="flex min-w-0 items-center justify-between gap-4 rounded-lg border border-[#2a3a52]/30 bg-gradient-to-r from-[#1a2332]/40 to-[#141b2a]/40 px-3 py-2">
                <span className="inline-flex items-center gap-2 font-semibold text-[#a6aec1]">
                  <Activity className="h-3.5 w-3.5 text-[#8a93a8]" />
                  {t("quota.dev_cores", { defaultValue: "Dev core-hours" })}
                </span>
                <span className="whitespace-nowrap font-mono text-[12px] font-semibold text-[#aeb6c9] tabular-nums">
                  {formatCoreHours(quota.dev_used_sec ?? null)} /{" "}
                  {formatCoreHours(quota.dev_total_sec ?? null)}
                </span>
              </div>
            </div>

            {percentage != null && (
              <div className="mt-auto space-y-1.5 pt-4">
                <div className="flex items-center justify-between gap-2 text-[11px] leading-none text-[#9aa3b7]">
                  <span className="font-medium tracking-tight">
                    {t("quota.used", { defaultValue: "Used" })}
                  </span>
                  <span className="shrink-0 font-mono font-semibold text-[#aeb6c9]">
                    {formatCredits(used)} / {formatCredits(total)}
                  </span>
                </div>
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-[#202838]/85">
                  <div
                    className={cn(
                      "h-full rounded-full",
                      percentage >= 90
                        ? "bg-[#d95473]/80"
                        : percentage >= 75
                          ? "bg-[#d59a25]/80"
                          : "bg-[#6572ff]/75"
                    )}
                    style={{ width: `${Math.min(percentage, 100)}%` }}
                  />
                </div>
                <div className="flex justify-end font-mono text-[10px] font-semibold leading-none text-[#9aa3b7]">
                  <span>
                    {percentage >= 0.01
                      ? `${percentage.toFixed(2)}%`
                      : percentage > 0
                        ? "<0.01%"
                        : "0%"}
                  </span>
                </div>
              </div>
            )}
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center font-mono text-[12px] text-[#6b7488]">
            {failed
              ? t("quota.unavailable", { defaultValue: "No data" })
              : t("quota.loading", { defaultValue: "Loading..." })}
          </div>
        )}
      </div>
    </div>
  );
}
