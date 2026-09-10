/**
 * 挖矿指标格式化纯函数（无 React 依赖，方便 node --test 覆盖）。
 * H/s 自适应单位：H → kH → MH → GH → TH → PH。
 */
export function formatHashrate(hs: number | null | undefined): string {
  if (hs == null || !isFinite(hs) || hs <= 0) return "0 H/s";
  const units = ["H/s", "kH/s", "MH/s", "GH/s", "TH/s", "PH/s"];
  let value = hs;
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit++;
  }
  const digits = value >= 100 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(digits)} ${units[unit]}`;
}

export type MiningLive = {
  algorithm?: string;
  hashrate_1min?: number;
};

export type MiningStatusPill = {
  name: string;
  up: boolean;
  metric: string;
  title: string;
};

/**
 * 节点卡片挖矿状态标识：有 live mining 才显示，风格对齐 CNB-AI-反代 pill。
 * 不算力（<=0）视为矿工上报但未在挖，红点 + 0 H/s；不把钱包写进 title。
 */
export function miningStatusPill(
  mining: MiningLive | null | undefined,
): MiningStatusPill | null {
  if (!mining || typeof mining !== "object") return null;
  const hs = Number(mining.hashrate_1min);
  const up = Number.isFinite(hs) && hs > 0;
  const name = String(mining.algorithm ?? "").trim() || "Mining";
  const metric = formatHashrate(up ? hs : 0);
  return {
    name,
    up,
    metric,
    title: up ? `${name} · ${metric}` : `${name} · idle`,
  };
}
