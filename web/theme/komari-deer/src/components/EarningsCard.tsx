"use client";

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Bitcoin, Coins, TrendingUp } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { useRPC2Call } from "@/contexts/RPC2Context";

/**
 * 账户级挖矿收益卡（Dashboard 顶部，风格与节点卡一致）。
 * 数据 = public:getMiningEarnings（hub 后台轮询 Kryptex 余额页的小时快照）。
 * 未启用（无快照）时整卡不渲染，零打扰。
 */
interface EarningsResp {
  enabled: boolean;
  latest?: {
    total_btc: number;
    available_btc: number;
    unpaid_btc: number;
    day_earned_btc: number;
    fetched_at: string;
  };
  hourly?: Array<{ hour_utc: string; earned_btc: number; total_btc: number }>;
}

const BTC_EIGHT = 1e8;

export function EarningsCard() {
  const { t } = useTranslation();
  const { call } = useRPC2Call();
  const [data, setData] = useState<EarningsResp | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const resp = await call<any, EarningsResp>("public:getMiningEarnings");
        if (!cancelled) setData(resp);
      } catch {
        // 功能未启用/旧版 hub：静默隐藏
      }
    };
    load();
    const timer = setInterval(load, 5 * 60 * 1000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [call]);

  if (!data?.enabled || !data.latest) return null;
  const { latest, hourly } = data;

  // 近 24h 增速 = 最近快照 - 25 小时前快照（不足则取首个样本）
  let trend = 0;
  if (hourly && hourly.length > 1) {
    const lastTotal = hourly[hourly.length - 1].total_btc;
    const base =
      hourly.length >= 25 ? hourly[hourly.length - 25].total_btc : hourly[0].total_btc;
    trend = Math.max(0, lastTotal - base);
  }

  const items = [
    {
      icon: Bitcoin,
      label: t("earnings.total", { defaultValue: "总余额" }),
      value: latest.total_btc,
    },
    {
      icon: Coins,
      label: t("earnings.available", { defaultValue: "可用" }),
      value: latest.available_btc,
    },
    {
      icon: TrendingUp,
      label: t("earnings.day", { defaultValue: "今日收益" }),
      value: latest.day_earned_btc,
    },
    {
      icon: TrendingUp,
      label: t("earnings.trend24h", { defaultValue: "24h 增速" }),
      value: trend,
    },
  ];

  return (
    <Card className="w-full">
      <CardContent className="p-4">
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          {items.map(({ icon: Icon, label, value }) => (
            <div key={label} className="flex items-center gap-3 min-w-0">
              <div className="rounded-lg bg-primary/10 p-2 shrink-0">
                <Icon className="h-5 w-5 text-primary" />
              </div>
              <div className="min-w-0">
                <div className="text-xs text-muted-foreground truncate">{label}</div>
                <div
                  className="text-sm font-semibold truncate"
                  title={`${value.toFixed(8)} BTC`}
                >
                  {(value * 1000).toFixed(5)} mBTC
                </div>
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
