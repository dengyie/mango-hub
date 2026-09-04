"use client";

import { useEffect, useState } from "react";

/**
 * CNB 额度数据源：消费 hk /ops/quota 公开 JSON（CNB cron 流水线每 5 分钟现签令牌同步，hk 零令牌）。
 * updated_at 停滞 >30 分钟视为同步中断（cron 正常周期 5 分钟）。
 * 消费方：instance/QuotaChart（LoadChart embedded 弹窗内的 CNB AI 额度卡）。
 */

const QUOTA_URL = "https://cnb-ai.mangoqwq.com/ops/quota";
const STALE_MS = 30 * 60 * 1000;

export interface QuotaSnapshot {
  updated_at: string;
  credit_total_milli: number | null;
  credit_free_milli: number | null;
  credit_used_milli: number | null;
  dev_used_sec: number | null;
  ci_used_sec: number | null;
  dev_total_sec: number | null;
  dev_free_sec: number | null;
}

export function useCnbQuota() {
  const [quota, setQuota] = useState<QuotaSnapshot | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let alive = true;
    const fetchQuota = async () => {
      try {
        const res = await fetch(QUOTA_URL, { cache: "no-store" });
        if (!res.ok) throw new Error(`quota sync http ${res.status}`);
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

  return {
    quota,
    failed,
    stale: quota != null && Date.now() - new Date(quota.updated_at).getTime() > STALE_MS,
  };
}
