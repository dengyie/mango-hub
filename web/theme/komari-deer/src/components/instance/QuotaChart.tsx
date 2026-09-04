"use client";

import { useTranslation } from "react-i18next";
import { Card, CardContent } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useCnbQuota } from "@/components/CnbQuotaCard";

/**
 * CNB AI 额度卡（LoadChart embedded 弹窗专属）：
 * 消费 hk /ops/quota 公开 JSON（CNB cron 流水线每 5 分钟同步），展示
 * AI Credits 已用/剩余/总量 + 开发核时 + CI 核时进度条。
 */

function formatCredits(milli: number | null | undefined): string {
  if (milli == null || !Number.isFinite(milli)) return "-";
  return (milli / 1000).toLocaleString(undefined, { maximumFractionDigits: 2 });
}

function formatCoreHours(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return "-";
  return `${(seconds / 3600).toLocaleString(undefined, { maximumFractionDigits: 1 })}h`;
}

const QuotaChart = () => {
  const { t } = useTranslation();
  const { quota, failed, stale } = useCnbQuota();

  const total = quota?.credit_total_milli ?? null;
  const used = quota?.credit_used_milli ?? null;
  const creditPercent =
    total && used != null && total > 0 ? (used / total) * 100 : null;
  const devPercent =
    quota?.dev_total_sec && quota.dev_total_sec > 0 && quota.dev_used_sec != null
      ? (quota.dev_used_sec / quota.dev_total_sec) * 100
      : null;
  const ciPercent =
    quota?.dev_total_sec != null &&
    quota.dev_total_sec > 0 &&
    quota.ci_used_sec != null
      ? ((quota.ci_used_sec / 576000) * 100)
      : null;

  const toneClass = (p: number | null) =>
    p == null
      ? "text-foreground"
      : p >= 90
        ? "text-red-400"
        : p >= 75
          ? "text-amber-400"
          : "text-foreground";

  const barColor = (p: number | null) =>
    p == null
      ? "bg-primary"
      : p >= 90
        ? "bg-red-400"
        : p >= 75
          ? "bg-amber-400"
          : "bg-primary";

  return (
    <Card className="w-full max-w-full min-w-0 flex flex-col h-full gap-4">
      <CardContent className="p-4">
        <div className="flex justify-between items-start mb-2 h-[80px]">
          <div className="flex flex-col justify-center gap-1">
            <label className="text-xl font-bold">
              {t("quota.title", { defaultValue: "CNB AI Quota" })}
            </label>
            {quota?.updated_at && (
              <span className="text-[11px] text-muted-foreground">
                {t("quota.updated_at", { defaultValue: "Updated" })}:{" "}
                {new Date(quota.updated_at).toLocaleString([], {
                  month: "2-digit",
                  day: "2-digit",
                  hour: "2-digit",
                  minute: "2-digit",
                })}
                {stale
                  ? ` · ${t("quota.stale", { defaultValue: "Sync interrupted" })}`
                  : ""}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2 h-full">
            <div className="text-sm text-muted-foreground text-right">
              {quota && total != null ? (
                <>
                  <div className="text-foreground font-semibold">
                    {formatCredits(used)} / {formatCredits(total)}
                  </div>
                  <div>credits</div>
                </>
              ) : (
                <div>{t("quota.unavailable", { defaultValue: "No data" })}</div>
              )}
            </div>
          </div>
        </div>

        {quota && total != null ? (
          <div className="flex flex-col gap-4">
            {/* AI Credits 进度条 */}
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{t("quota.used", { defaultValue: "Used" })}</span>
                <span className={toneClass(creditPercent)}>
                  {creditPercent != null
                    ? creditPercent >= 0.01
                      ? `${creditPercent.toFixed(2)}%`
                      : creditPercent > 0
                        ? "<0.01%"
                        : "0%"
                    : "-"}
                </span>
              </div>
              <Progress
                value={creditPercent ?? 0}
                className="h-2"
                indicatorClassName={barColor(creditPercent)}
              />
              <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                <span>
                  {t("quota.remaining", { defaultValue: "Remaining" })}:{" "}
                  <span className="text-foreground font-medium">
                    {formatCredits(total != null && used != null ? total - used : null)}
                  </span>
                </span>
                <span>
                  {t("quota.total", { defaultValue: "Total" })}:{" "}
                  <span className="text-foreground font-medium">
                    {formatCredits(total)}
                  </span>
                </span>
              </div>
            </div>

            {/* 开发核时 */}
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{t("quota.dev_cores", { defaultValue: "Dev core-hours" })}</span>
                <span className={toneClass(devPercent)}>
                  {formatCoreHours(quota.dev_used_sec)} /{" "}
                  {formatCoreHours(quota.dev_total_sec)}
                </span>
              </div>
              <Progress
                value={devPercent ?? 0}
                className="h-2"
                indicatorClassName={barColor(devPercent)}
              />
            </div>

            {/* CI 核时 */}
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{t("quota.ci_cores", { defaultValue: "CI core-hours" })}</span>
                <span className={toneClass(ciPercent)}>
                  {formatCoreHours(quota.ci_used_sec)}
                </span>
              </div>
              <Progress
                value={ciPercent ?? 0}
                className="h-2"
                indicatorClassName={barColor(ciPercent)}
              />
            </div>
          </div>
        ) : (
          <div className="py-8 text-center text-sm text-muted-foreground">
            {failed
              ? t("quota.unavailable", { defaultValue: "No data" })
              : t("quota.loading", { defaultValue: "Loading..." })}
          </div>
        )}
      </CardContent>
    </Card>
  );
};

export default QuotaChart;
