/**
 * CNB 额度快照的纯逻辑：格式化、百分比、阈值分桶。
 * 与后端契约：ai-proxy 仓 .cnb.yml quota-sync stage 的 jq 组装字段，
 * 字段变更必须两侧同步（ci_total_sec 与 dev_total_sec 同源——CNB 免费核时为 dev+CI 共享池）。
 */

export interface QuotaSnapshot {
  updated_at: string;
  credit_total_milli: number | null;
  credit_free_milli: number | null;
  credit_used_milli: number | null;
  dev_used_sec: number | null;
  ci_used_sec: number | null;
  dev_total_sec: number | null;
  dev_free_sec: number | null;
  ci_total_sec: number | null;
}

/** milli-credit → credit 字符串（保留两位小数，千分位随 locale） */
export function formatCredits(milli: number | null | undefined): string {
  if (milli == null || !Number.isFinite(milli)) return "-";
  return (milli / 1000).toLocaleString(undefined, { maximumFractionDigits: 2 });
}

/** 核秒 → 核时字符串（一位小数） */
export function formatCoreHours(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return "-";
  return `${(seconds / 3600).toLocaleString(undefined, { maximumFractionDigits: 1 })}h`;
}

/** used/total 百分比；total 缺失或 ≤0 时返回 null（调用方隐藏进度条） */
export function quotaPercent(
  used: number | null | undefined,
  total: number | null | undefined
): number | null {
  if (used == null || !Number.isFinite(used)) return null;
  if (total == null || !Number.isFinite(total) || total <= 0) return null;
  return (used / total) * 100;
}

/** 进度条视觉钳制（used > total 时防止 translateX 溢出） */
export function clampPercent(p: number | null): number {
  if (p == null || !Number.isFinite(p)) return 0;
  return Math.min(100, Math.max(0, p));
}

/** 用量告警分桶：≥90% 红、≥75% 黄、否则默认色 */
export function quotaTone(
  p: number | null
): "danger" | "warning" | "default" {
  if (p == null) return "default";
  if (p >= 90) return "danger";
  if (p >= 75) return "warning";
  return "default";
}

/** tailwind class 映射（文本色 / 进度条填充色） */
export function toneTextClass(p: number | null): string {
  switch (quotaTone(p)) {
    case "danger":
      return "text-red-400";
    case "warning":
      return "text-amber-400";
    default:
      return "text-foreground";
  }
}

export function toneBarClass(p: number | null): string {
  switch (quotaTone(p)) {
    case "danger":
      return "bg-red-400";
    case "warning":
      return "bg-amber-400";
    default:
      return "bg-primary";
  }
}

/** 百分比展示：≥0.01% 两位小数；(0, 0.01%) 显示 <0.01%；0 与 null 特判 */
export function formatPercent(p: number | null): string {
  if (p == null || !Number.isFinite(p)) return "-";
  if (p >= 0.01) return `${p.toFixed(2)}%`;
  if (p > 0) return "<0.01%";
  return "0%";
}

/** 快照更新时间距 now 是否超过 stale 阈值（默认 30 分钟，cron 周期 5 分钟） */
export function isStale(
  updatedAt: string | undefined,
  now: number,
  staleMs = 30 * 60 * 1000
): boolean {
  if (!updatedAt) return false;
  const t = new Date(updatedAt).getTime();
  if (!Number.isFinite(t)) return false;
  return now - t > staleMs;
}
