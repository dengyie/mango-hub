"use client";

import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Wallet } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * CNB AI 额度卡：消费 hk /ops/quota 公开 JSON（CNB cron 流水线每 5 分钟现签令牌同步，hk 零令牌）。
 * 数据字段：credit_total_milli / credit_used_milli（milli-credit）、dev_used_sec / dev_total_sec（核秒）。
 * updated_at 停滞 >30 分钟视为同步中断（cron 正常周期 5 分钟）。
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

export function CnbQuotaCard() {
  const [t] = useTranslation();
  const [mounted, setMounted] = useState(false);
  const [quota, setQuota] = useState<QuotaSnapshot | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setMounted(true);
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

  if (!mounted) return null;

  const stale =
    quota != null &&
    Date.now() - new Date(quota.updated_at).getTime() > STALE_MS;

  const total = quota?.credit_total_milli ?? null;
  const used = quota?.credit_used_milli ?? null;
  const percentage = total && used != null ? (used / total) * 100 : null;

  return (
    <div className="relative flex min-h-0 w-full overflow-hidden rounded-[14px] border border-[#3a4a66]/60 bg-[linear-gradient(135deg,rgba(20,26,42,0.95)_0%,rgba(14,19,32,0.98)_50%,rgba(10,14,26,0.99)_100%)] p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.06),0_8px_24px_rgba(0,0,0,0.4)] select-none">
      <div className="flex w-full flex-col gap-3">
        <div className="flex items-center justify-between">
          <span className="inline-flex items-center gap-2 font-bold tracking-tight text-base">
            <Wallet className="h-4 w-4 text-[#8a93a8]" />
            {t("quota.title", { defaultValue: "CNB AI Quota" })}
          </span>
          <span
            className={cn(
              "font-mono text-[10px] font-semibold",
              failed || stale ? "text-[#d59a25]" : "text-[#6b7488]"
            )}
          >
            {failed
              ? t("quota.unavailable", { defaultValue: "No data" })
              : stale
                ? t("quota.stale", { defaultValue: "Sync interrupted" })
                : ""}
          </span>
        </div>

        {quota && total != null ? (
          <>
            <div className="grid grid-cols-3 gap-2 text-center font-mono text-[13px] font-extrabold tabular-nums">
              <div className="flex flex-col gap-0.5 rounded-lg bg-gradient-to-r from-[#1a2332]/40 to-[#141b2a]/40 py-2">
                <span className="text-[10px] font-semibold uppercase tracking-wide text-[#9aa3b7]">
                  {t("quota.used", { defaultValue: "Used" })}
                </span>
                <span className="text-[#d95473]">{formatCredits(used)}</span>
              </div>
              <div className="flex flex-col gap-0.5 rounded-lg bg-gradient-to-r from-[#1a2332]/40 to-[#141b2a]/40 py-2">
                <span className="text-[10px] font-semibold uppercase tracking-wide text-[#9aa3b7]">
                  {t("quota.remaining", { defaultValue: "Remaining" })}
                </span>
                <span className="text-[#00df7c]">
                  {formatCredits(total != null && used != null ? total - used : null)}
                </span>
              </div>
              <div className="flex flex-col gap-0.5 rounded-lg bg-gradient-to-r from-[#1a2332]/40 to-[#141b2a]/40 py-2">
                <span className="text-[10px] font-semibold uppercase tracking-wide text-[#9aa3b7]">
                  {t("quota.total", { defaultValue: "Total" })}
                </span>
                <span className="text-[#aeb6c9]">{formatCredits(total)}</span>
              </div>
            </div>

            {percentage != null && (
              <div className="space-y-1">
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
                <div className="flex items-center justify-between font-mono text-[10px] font-semibold leading-none text-[#9aa3b7]">
                  <span>
                    {t("quota.dev_cores", { defaultValue: "Dev core-hours" })}:{" "}
                    {formatCoreHours(quota.dev_used_sec ?? null)}
                    {quota.dev_total_sec ? ` / ${formatCoreHours(quota.dev_total_sec)}` : ""}
                  </span>
                  <span>{percentage.toFixed(1)}%</span>
                </div>
              </div>
            )}
          </>
        ) : (
          <div className="py-3 text-center font-mono text-[12px] text-[#6b7488]">
            {t("quota.unavailable", { defaultValue: "No data" })}
          </div>
        )}
      </div>
    </div>
  );
}
