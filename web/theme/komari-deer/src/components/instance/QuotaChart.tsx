"use client";

import { useTranslation } from "react-i18next";
import { Card, CardContent } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useCnbQuota } from "@/hooks/useCnbQuota";
import { useCnbUsage } from "@/hooks/useCnbUsage";
import {
  clampPercent,
  formatCoreHours,
  formatCredits,
  formatPercent,
  quotaPercent,
  toneBarClass,
  toneTextClass,
} from "@/utils/quotaHelper";
import {
  formatCount,
  formatTokens,
  recentDays,
  sumDays,
  todayUsage,
} from "@/utils/usageHelper";

/**
 * CNB AI 额度卡（LoadChart embedded 弹窗专属）：
 * 消费 hk /ops/quota 公开 JSON（CNB cron 流水线每 5 分钟同步），展示
 * AI Credits 已用/剩余/总量 + 开发核时 + CI 核时进度条；
 * 尾部 token 用量区消费 /ops/usage（boot 增量对账按天桶），展示
 * 今日（UTC+8）/累计/近 7 日趋势。两数据源独立失败语义，互不影响。
 * 百分比/格式化/阈值逻辑在 utils/quotaHelper.ts 与 utils/usageHelper.ts
 * （纯函数，tests/quotaFormat.test.mjs、tests/usageFormat.test.mjs 覆盖）。
 */

const QuotaChart = () => {
  const { t } = useTranslation();
  const { quota, failed, stale } = useCnbQuota();
  const { usage, failed: usageFailed, stale: usageStale } = useCnbUsage();

  const total = quota?.credit_total_milli ?? null;
  const used = quota?.credit_used_milli ?? null;
  const creditPercent = quotaPercent(used, total);
  // CI 与 dev 共享同一免费核时池，后端 ci_total_sec 与 dev_total_sec 同源；
  // 独立守卫：总量缺失时只显示用量、不渲染进度条
  const devPercent = quotaPercent(quota?.dev_used_sec, quota?.dev_total_sec);
  const ciPercent = quotaPercent(quota?.ci_used_sec, quota?.ci_total_sec);

  const rows: {
    label: string;
    percent: number | null;
    value: string;
    detail?: string;
  }[] = [
    {
      label: t("quota.used", { defaultValue: "Used" }),
      percent: creditPercent,
      value: formatPercent(creditPercent),
    },
    {
      label: t("quota.dev_cores", { defaultValue: "Dev core-hours" }),
      percent: devPercent,
      value: `${formatCoreHours(quota?.dev_used_sec)} / ${formatCoreHours(quota?.dev_total_sec)}`,
    },
    {
      label: t("quota.ci_cores", { defaultValue: "CI core-hours" }),
      percent: ciPercent,
      value: `${formatCoreHours(quota?.ci_used_sec)}${ciPercent != null ? ` / ${formatCoreHours(quota?.ci_total_sec)}` : ""}`,
    },
  ];

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
            {rows.map((row) => (
              <div key={row.label} className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                  <span>{row.label}</span>
                  <span className={toneTextClass(row.percent)}>{row.value}</span>
                </div>
                {row.percent != null && (
                  <Progress
                    value={clampPercent(row.percent)}
                    className="h-2"
                    indicatorClassName={toneBarClass(row.percent)}
                  />
                )}
                {row.detail != null && (
                  <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                    <span>{row.detail}</span>
                  </div>
                )}
              </div>
            ))}
            <div className="flex items-center justify-between text-[11px] text-muted-foreground">
              <span>
                {t("quota.remaining", { defaultValue: "Remaining" })}:{" "}
                <span className="text-foreground font-medium">
                  {formatCredits(
                    total != null && used != null ? total - used : null
                  )}
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
        ) : (
          <div className="py-8 text-center text-sm text-muted-foreground">
            {failed
              ? t("quota.unavailable", { defaultValue: "No data" })
              : t("quota.loading", { defaultValue: "Loading..." })}
          </div>
        )}

        <TokenUsageSection
          usage={usage}
          failed={usageFailed}
          stale={usageStale}
        />
      </CardContent>
    </Card>
  );
};

/**
 * token 用量区（额度卡弹窗内第二区块）：消费 hk /ops/usage 公开 JSON，
 * 展示今日（UTC+8）prompt/completion/请求数 + 全期累计 + 近 7 日趋势。
 * 独立失败语义：usage 拉取失败不影响上方额度数据展示。
 */
const TokenUsageSection = ({
  usage,
  failed,
  stale,
}: {
  usage: ReturnType<typeof useCnbUsage>["usage"];
  failed: boolean;
  stale: boolean;
}) => {
  const { t } = useTranslation();
  const today = todayUsage(usage);
  const total = sumDays(usage);
  const trend = recentDays(usage, 7);
  const trendMax = Math.max(1, ...trend.map(([, d]) => d.prompt + d.completion));
  const hasData = usage != null && Object.keys(usage.days).length > 0;

  return (
    <div className="mt-4 pt-4 border-t flex flex-col gap-3">
      <div className="flex items-baseline justify-between">
        <label className="text-sm font-semibold">
          {t("usage.title", { defaultValue: "Token Usage" })}
        </label>
        <span className="text-[11px] text-muted-foreground">
          {stale
            ? t("quota.stale", { defaultValue: "Sync interrupted" })
            : t("quota.synced", { defaultValue: "Synced every 5 min" })}
        </span>
      </div>

      {hasData ? (
        <>
          <div className="grid grid-cols-2 gap-2 text-xs">
            <div className="flex flex-col gap-0.5">
              <span className="text-muted-foreground">
                {t("usage.today", { defaultValue: "Today (UTC+8)" })}
              </span>
              <span className="font-medium">
                {formatTokens(today.prompt)} ↑ / {formatTokens(today.completion)} ↓
              </span>
              <span className="text-[11px] text-muted-foreground">
                {t("usage.requests", { defaultValue: "Requests" })}:{" "}
                {formatCount(today.requests)}
                {today.errors > 0
                  ? ` · ${t("usage.errors", { defaultValue: "Errors" })}: ${formatCount(today.errors)}`
                  : ""}
              </span>
            </div>
            <div className="flex flex-col gap-0.5">
              <span className="text-muted-foreground">
                {t("usage.cumulative", { defaultValue: "Cumulative" })}
              </span>
              <span className="font-medium">
                {formatTokens(total.prompt)} ↑ / {formatTokens(total.completion)} ↓
              </span>
              <span className="text-[11px] text-muted-foreground">
                {t("usage.requests", { defaultValue: "Requests" })}:{" "}
                {formatCount(total.requests)}
              </span>
            </div>
          </div>

          {trend.length > 1 && (
            <div className="flex flex-col gap-1.5">
              <span className="text-[11px] text-muted-foreground">
                {t("usage.trend", { defaultValue: "Last 7 days" })}
              </span>
              <div className="flex items-end gap-1 h-10" aria-hidden>
                {trend.map(([day, d]) => {
                  const v = d.prompt + d.completion;
                  const h = Math.max(4, Math.round((v / trendMax) * 100));
                  return (
                    <div
                      key={day}
                      className="flex-1 flex flex-col justify-end items-center gap-0.5 min-w-0"
                      title={`${day}: ${formatTokens(d.prompt)} / ${formatTokens(d.completion)}`}
                    >
                      <div
                        className="w-full rounded-sm bg-primary/70"
                        style={{ height: `${h}%` }}
                      />
                      <span className="text-[9px] text-muted-foreground truncate w-full text-center">
                        {day.slice(5)}
                      </span>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </>
      ) : (
        <div className="py-2 text-center text-xs text-muted-foreground">
          {failed
            ? t("quota.unavailable", { defaultValue: "No data" })
            : t("quota.loading", { defaultValue: "Loading..." })}
        </div>
      )}
    </div>
  );
};

export default QuotaChart;
