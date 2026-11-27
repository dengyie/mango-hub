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

/** power_governor 自适应功率档位(full/eco/pause);来源 agent state.json。 */
export type MiningGovernor = {
  tier?: string;
  idle_sec?: number;
  gpu_pct?: number;
};

export type MiningLive = {
  algorithm?: string;
  hashrate_1min?: number;
  governor?: MiningGovernor;
};

export type GovernorTone = "ok" | "warn" | "alert" | "muted";

export type GovernorBadge = {
  tier: string;
  tone: GovernorTone;
  /** i18n key;组件用 t(key) 渲染,缺翻译时回落到中文 defaultValue。 */
  i18nKey: string;
};

/** 徽标配色(与节点卡片 online/offline 同一套色板),集中一处便于卡片与详情页共用。 */
export const GOVERNOR_TONE_CLASS: Record<GovernorTone, string> = {
  ok: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  warn: "bg-amber-500/10 text-amber-700 dark:text-amber-400",
  alert: "bg-rose-500/10 text-rose-600 dark:text-rose-400",
  muted: "bg-muted",
};

/**
 * 档位 → 徽标。full=满速(绿), eco=节能降功率(琥珀), pause=让卡暂停(红)。
 * 空/未知档位返回 null(不显示徽标),避免给非自适应节点平白加噪声。
 */
export function governorBadge(
  gov: MiningGovernor | null | undefined,
): GovernorBadge | null {
  const tier = String(gov?.tier ?? "").trim().toLowerCase();
  if (!tier) return null;
  if (tier === "full") return { tier, tone: "ok", i18nKey: "mining.govFull" };
  if (tier === "eco") return { tier, tone: "warn", i18nKey: "mining.govEco" };
  if (tier === "pause") return { tier, tone: "alert", i18nKey: "mining.govPause" };
  return { tier, tone: "muted", i18nKey: "mining.govUnknown" };
}

export type MiningStatusPill = {
  name: string;
  up: boolean;
  metric: string;
  title: string;
  /** 自适应功率档位徽标;无 governor 数据时该键不存在(保持老节点 DTO 形状不变)。 */
  governor?: GovernorBadge;
};

/**
 * 节点卡片挖矿状态标识：有 live mining 才显示，风格对齐 CNB-AI-反代 pill。
 * 不算力（<=0）视为矿工上报但未在挖，红点 + 0 H/s；不把钱包写进 title。
 */
export function miningStatusPill(
  mining: MiningLive | null | undefined,
): MiningStatusPill | null {
  if (!mining || typeof mining !== "object") return null;
  // 只认真正的 number:Number(null) === 0,会把「没有算力字段」误判成
  // 「矿工在跑但 0 H/s」,从而给只有档位的块造出假 pill。
  const rawHs = mining.hashrate_1min;
  const hs = typeof rawHs === "number" && Number.isFinite(rawHs) ? rawHs : undefined;
  const up = hs !== undefined && hs > 0;
  const algorithm = String(mining.algorithm ?? "").trim();
  // 只有档位的块(pause 档停掉矿工,DTO 里既无 algorithm 也无 hashrate)不是「矿工在跑但没算力」,
  // 不能渲染成 「Mining · 0 H/s」——档位由 governorBadge 单独表达。
  if (!algorithm && hs === undefined) return null;
  const name = algorithm || "Mining";
  const metric = formatHashrate(up ? hs : 0);
  const governor = governorBadge(mining.governor);
  return {
    name,
    up,
    metric,
    title: up ? `${name} · ${metric}` : `${name} · idle`,
    ...(governor ? { governor } : {}),
  };
}
