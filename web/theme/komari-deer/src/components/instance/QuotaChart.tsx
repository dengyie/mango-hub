"use client";

import { useTranslation } from "react-i18next";
import { Card, CardContent } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useCnbQuota } from "@/hooks/useCnbQuota";
import {
  clampPercent,
  formatCoreHours,
  formatCredits,
  formatPercent,
  quotaPercent,
  toneBarClass,
  toneTextClass,
} from "@/utils/quotaHelper";

/**
 * CNB AI 额度卡（LoadChart embedded 弹窗专属）：
 * 消费 hk /ops/quota 公开 JSON（CNB cron 流水线每 5 分钟同步），展示
 * AI Credits 已用/剩余/总量 + 开发核时 + CI 核时进度条。
 * 百分比/格式化/阈值逻辑在 utils/quotaHelper.ts（纯函数，tests/quotaFormat.test.mjs 覆盖）。
 */

const QuotaChart = () => {
  const { t } = useTranslation();
  const { quota, failed, stale } = useCnbQuota();

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
      </CardContent>
    </Card>
  );
};

export default QuotaChart;
