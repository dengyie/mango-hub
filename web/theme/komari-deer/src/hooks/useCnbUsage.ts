"use client";

import { useEffect, useState } from "react";
import { isUsageStale, type UsageSnapshot } from "@/utils/usageHelper";

/**
 * CNB token 用量数据源：消费 hk /ops/usage 公开 JSON（cron usage-sync 每 5 分钟同步）。
 * - 每 5 分钟轮询一次（与 cron 周期一致）。
 * - updated_at 停滞 >30 分钟视为同步中断（stale；tick 每分钟刷新一次，
 *   保证页面无其他渲染源时 stale 也能翻转）。
 * - 响应缺 updated_at / days 视为无效快照（按 failed 处理，保留上次成功数据）。
 * 消费方：instance/QuotaChart（LoadChart embedded 弹窗内的 CNB AI 额度卡 token 区）。
 * 字段契约见 usageHelper.ts 头注释。
 */

const USAGE_URL = "https://cnb-ai.mangoqwq.com/ops/usage";
const POLL_MS = 5 * 60 * 1000;
const STALE_TICK_MS = 60 * 1000;

export type { UsageSnapshot } from "@/utils/usageHelper";

export function useCnbUsage() {
  const [usage, setUsage] = useState<UsageSnapshot | null>(null);
  const [failed, setFailed] = useState(false);
  // 时间流逝驱动：stale 是 now 的函数，仅靠轮询 setState 刷新时，
  // 无其他渲染源的页面会永远看不到 stale 翻转
  const [staleTick, setStaleTick] = useState(0);

  useEffect(() => {
    let alive = true;
    const fetchUsage = async () => {
      try {
        const res = await fetch(USAGE_URL, { cache: "no-store" });
        if (!res.ok) throw new Error(`usage sync http ${res.status}`);
        const data = (await res.json()) as UsageSnapshot;
        // 半残快照（同步链路任一环缺失关键字段）按失败处理，不覆盖上次成功值
        const valid =
          data &&
          typeof data.updated_at === "string" &&
          data.days != null &&
          typeof data.days === "object";
        if (!valid) throw new Error("usage snapshot incomplete");
        if (alive) {
          setUsage(data);
          setFailed(false);
        }
      } catch {
        if (alive) setFailed(true);
      }
    };
    void fetchUsage();
    const poll = setInterval(fetchUsage, POLL_MS);
    const tick = setInterval(() => setStaleTick((n) => n + 1), STALE_TICK_MS);
    return () => {
      alive = false;
      clearInterval(poll);
      clearInterval(tick);
    };
  }, []);

  // staleTick 仅作订阅依赖（触发每分钟重算），不参与计算
  void staleTick;
  return {
    usage,
    failed,
    stale: isUsageStale(usage?.updated_at, Date.now()),
  };
}
