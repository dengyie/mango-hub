"use client";

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Card, CardContent } from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  XAxis,
  YAxis,
} from "recharts";
import type { MiningRecord } from "@/lib/historyCache";
import { formatHashrate } from "@/utils/miningHelper";

type LoadChartFloatProps = {
  records: MiningRecord[];
  timeFormatter: (value: any, index: number) => string;
  labelFormatter: (value: any) => string;
  margin?: { top: number; right: number; bottom: number; left: number };
};

const defaultMargin = { top: 0, right: 16, bottom: 0, left: 16 };
const colors = ["#F38181", "#FCE38A", "#EAFFD0", "#95E1D3"];

/**
 * 挖矿监控卡（节点详情页 LoadChart 网格一员，风格与 CPU/RAM 卡一致）：
 * 数据 = /api/records/load 的 mining_records（hub 端 mining.* tagged 指标重建）。
 * 算力面积图 + 功耗/温度折线 + 份额统计；无数据时整卡不渲染（非矿机零打扰）。
 */
const MiningChart = ({
  records,
  timeFormatter,
  labelFormatter,
  margin = defaultMargin,
}: LoadChartFloatProps) => {
  const { t } = useTranslation();

  const latest = records.length > 0 ? records[records.length - 1] : null;

  const chartData = useMemo(
    () =>
      records.map((r) => ({
        time: r.time,
        hashrate: r.hashrate,
        power: r.power,
        temperature: r.temperature,
        shares_valid: r.shares_valid,
        shares_stale: r.shares_stale,
        shares_invalid: r.shares_invalid,
        hw_errors: r.hw_errors,
        pool_latency: r.pool_latency,
      })),
    [records]
  );

  if (!latest) return null;

  return (
    <Card className="w-full max-w-full min-w-0 flex flex-col h-full gap-4">
      <CardContent className="p-4">
        <div className="flex justify-between items-start mb-2 h-[80px]">
          <div className="flex flex-col justify-center gap-1">
            <label className="text-xl font-bold">
              {t("mining.title", { defaultValue: "Mining" })}
            </label>
            <span className="text-xs text-muted-foreground truncate max-w-44">
              {latest.algorithm} · {latest.pool}
            </span>
          </div>
          <div className="flex flex-col items-end gap-0 text-sm">
            <label className="font-bold">{formatHashrate(latest.hashrate)}</label>
            <label className="text-muted-foreground">
              {latest.power > 0 ? `${latest.power.toFixed(1)} W` : "-"}
              {latest.temperature > 0 ? ` · ${latest.temperature.toFixed(0)}°C` : ""}
            </label>
            <label className="text-muted-foreground">
              {t("mining.shares", { defaultValue: "Shares" })}: {latest.shares_valid}
              {latest.shares_invalid > 0 ? ` / ✗${latest.shares_invalid}` : ""}
              {latest.hw_errors > 0 ? ` / HW:${latest.hw_errors}` : ""}
            </label>
          </div>
        </div>
        <ChartContainer
          config={{
            hashrate: {
              label: t("mining.hashrate", { defaultValue: "Hashrate" }),
              color: colors[0],
            },
            power: {
              label: "W",
              color: colors[3],
            },
            temperature: {
              label: "°C",
              color: colors[1],
            },
          }}
        >
          <AreaChart data={chartData} accessibilityLayer margin={margin}>
            <CartesianGrid vertical={false} />
            <XAxis
              dataKey="time"
              tickLine={false}
              tickFormatter={timeFormatter}
              interval={0}
            />
            <YAxis
              yAxisId="hashrate"
              tickLine={false}
              axisLine={false}
              tickFormatter={(value: number) => formatHashrate(value)}
              orientation="left"
              type="number"
              tick={{ dx: -10 }}
              mirror={true}
              domain={[0, "dataMax"]}
            />
            <YAxis
              yAxisId="aux"
              hide
              domain={[0, "dataMax"]}
            />
            <ChartTooltip
              cursor={false}
              formatter={(value, name) => {
                if (name === "hashrate") return formatHashrate(Number(value));
                if (name === "power") return `${Number(value).toFixed(1)} W`;
                if (name === "temperature") return `${Number(value).toFixed(0)}°C`;
                return `${value}`;
              }}
              content={
                <ChartTooltipContent
                  labelFormatter={labelFormatter}
                  indicator="dot"
                />
              }
            />
            <Area
              yAxisId="hashrate"
              dataKey="hashrate"
              animationDuration={0}
              stroke={colors[0]}
              fill={colors[0]}
              opacity={0.8}
              dot={false}
            />
            <Line
              yAxisId="aux"
              dataKey="power"
              animationDuration={0}
              stroke={colors[3]}
              dot={false}
            />
            <Line
              yAxisId="aux"
              dataKey="temperature"
              animationDuration={0}
              stroke={colors[1]}
              dot={false}
            />
          </AreaChart>
        </ChartContainer>
        <div className="grid grid-cols-3 gap-4 text-sm text-muted-foreground mt-2">
          <div className="text-center">
            <div className="font-medium">
              {t("mining.shares_valid", { defaultValue: "Valid" })}
            </div>
            <div className="text-lg font-bold text-foreground">
              {latest.shares_valid}
            </div>
          </div>
          <div className="text-center">
            <div className="font-medium">
              {t("mining.shares_stale", { defaultValue: "Stale" })}
            </div>
            <div className="text-lg font-bold text-foreground">
              {latest.shares_stale}
            </div>
          </div>
          <div className="text-center">
            <div className="font-medium">
              {t("mining.shares_invalid", { defaultValue: "Invalid" })}
            </div>
            <div className="text-lg font-bold text-foreground">
              {latest.shares_invalid}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default MiningChart;
