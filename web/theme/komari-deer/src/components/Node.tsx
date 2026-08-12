import React from "react";
import { useTranslation } from "react-i18next";
import { Activity, TrendingUp } from "lucide-react";
import type { TFunction } from "i18next";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import type { NodeBasicInfo } from "@/contexts/NodeListContext";
import type { LiveData, Record } from "../types/LiveData";
import { getOSImage, getOSName } from "@/utils";
import { formatBytes } from "@/utils/unitHelper";
import { type PingHistoryPoint, type PingStats, usePingStats } from "@/hooks/usePingStats";

import Flag from "./Flag";
import PriceTags from "./PriceTags";
import CircleChart from "./CircleChart";
import LoadChartFloat from "./LoadChartFloat";
import Tips from "./ui/tips";

/** Format seconds into readable uptime */
export function formatUptime(seconds: number, t: TFunction): string {
  if (!seconds || seconds < 0) return t("nodeCard.time_second", { val: 0 });
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  const parts = [];
  if (d) parts.push(`${d} ${t("nodeCard.time_day")}`);
  if (h) parts.push(`${h} ${t("nodeCard.time_hour")}`);
  if (m) parts.push(`${m} ${t("nodeCard.time_minute")}`);
  if (s || parts.length === 0) parts.push(`${s} ${t("nodeCard.time_second")}`);
  return parts.join(" ");
}

/** Compact uptime formatter: 72d 5h 25m 30s */
function formatUptimeCompact(seconds: number): string {
  if (!seconds || seconds < 0) return "0s";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  const parts = [];
  if (d) parts.push(`${d}d`);
  if (h) parts.push(`${h}h`);
  if (m) parts.push(`${m}m`);
  if (s || parts.length === 0) parts.push(`${s}s`);
  return parts.join(" ");
}

export function getTrafficPercentage(
  totalUp: number,
  totalDown: number,
  limit: number,
  type: "max" | "min" | "sum" | "up" | "down"
) {
  if (limit === 0) return 0;
  switch (type) {
    case "max":
      return (Math.max(totalUp, totalDown) / limit) * 100;
    case "min":
      return (Math.min(totalUp, totalDown) / limit) * 100;
    case "sum":
      return ((totalUp + totalDown) / limit) * 100;
    case "up":
      return (totalUp / limit) * 100;
    case "down":
      return (totalDown / limit) * 100;
    default:
      return 0;
  }
}

export function getTrafficUsed(
  totalUp: number,
  totalDown: number,
  type: "max" | "min" | "sum" | "up" | "down"
) {
  switch (type) {
    case "max":
      return Math.max(totalUp, totalDown);
    case "min":
      return Math.min(totalUp, totalDown);
    case "sum":
      return totalUp + totalDown;
    case "up":
      return totalUp;
    case "down":
      return totalDown;
    default:
      return 0;
  }
}

export function formatTrafficPercentage(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0%";
  if (value >= 100) return `${value.toFixed(1)}%`;
  if (value >= 10) return `${value.toFixed(1)}%`;
  if (value >= 1) return `${value.toFixed(2)}%`;
  if (value >= 0.01) return `${value.toFixed(3)}%`;
  return "<0.01%";
}

const COMPACT_PING_BAR_COUNT = 24;

function compactLatencyClass(latency: number | null) {
  if (latency === null) return "bg-[#252b39]";
  if (latency <= 80) return "bg-[#00b875]";
  if (latency <= 180) return "bg-[#f3a000]";
  return "bg-[#e64b73]";
}

function compactLossClass(loss: number | null) {
  if (loss === null) return "bg-[#252b39]";
  if (loss <= 1) return "bg-[#00b875]";
  if (loss <= 5) return "bg-[#f3a000]";
  return "bg-[#e64b73]";
}

function getNearestMetricValue(
  history: PingHistoryPoint[],
  index: number,
  metric: "latency" | "loss",
): number | null {
  const direct = history[index]?.[metric];
  if (typeof direct === "number") return direct;

  for (let offset = 1; offset < history.length; offset++) {
    const left = history[index - offset]?.[metric];
    if (typeof left === "number") return left;

    const right = history[index + offset]?.[metric];
    if (typeof right === "number") return right;
  }

  return null;
}

function compactMetricBars(history: PingHistoryPoint[], metric: "latency" | "loss") {
  const source = history.slice(-COMPACT_PING_BAR_COUNT);

  if (source.length === 0) {
    return Array.from({ length: COMPACT_PING_BAR_COUNT }, (_, index) => ({
      key: `empty-${metric}-${index}`,
      value: null,
      time: "",
    }));
  }

  return Array.from({ length: COMPACT_PING_BAR_COUNT }, (_, index) => {
    const sourceIndex =
      source.length === COMPACT_PING_BAR_COUNT
        ? index
        : Math.round((index / Math.max(COMPACT_PING_BAR_COUNT - 1, 1)) * (source.length - 1));
    const point = source[sourceIndex];

    return {
      key: `${metric}-${point?.time ?? "point"}-${index}`,
      value: getNearestMetricValue(source, sourceIndex, metric),
      time: point?.time ?? "",
    };
  });
}

function CompactMetricBarStrip({
  label,
  value,
  valueClassName,
  points,
  metric,
  t,
}: {
  label: string;
  value: string;
  valueClassName?: string;
  points: ReturnType<typeof compactMetricBars>;
  metric: "latency" | "loss";
  t: TFunction;
}) {
  return (
    <div className="min-w-0 rounded-[6px] border border-[#222b3a]/85 bg-[#0d1320]/80 px-2 py-1.5">
      <div className="mb-1 flex items-center justify-between gap-2 text-[10px] leading-none">
        <span className="truncate text-[#8f98ac]">{label}</span>
        <span className={cn("shrink-0 font-mono font-semibold text-[#cfd6e6]", valueClassName)}>
          {value}
        </span>
      </div>
      <div
        className="grid h-[16px] gap-0.5 overflow-hidden"
        style={{ gridTemplateColumns: `repeat(${Math.max(points.length, 1)}, minmax(0, 1fr))` }}
      >
        {points.map((point) => (
          <span
            key={point.key}
            className={cn(
              "min-w-0 rounded-[2px]",
              metric === "latency"
                ? compactLatencyClass(point.value)
                : compactLossClass(point.value)
            )}
            title={
              point.value === null
                ? t("nodeCard.noPingData")
                : metric === "latency"
                  ? `${Math.round(point.value)} ms`
                  : `${point.value.toFixed(1)}%`
            }
          />
        ))}
      </div>
    </div>
  );
}

function CompactPingTimeline({ pingStats, t }: { pingStats: PingStats; t: TFunction }) {
  if (!pingStats.hasData) {
    return (
      <div className="flex h-[62px] items-center justify-between text-[12px] select-none">
        <span className="font-medium text-[#8f98ac]">Ping Stats (24h)</span>
        <span className="text-[12px] italic text-[#848da3]">{t("nodeCard.noPingData")}</span>
      </div>
    );
  }

  const latencyBars = compactMetricBars(pingStats.history, "latency");
  const lossBars = compactMetricBars(pingStats.history, "loss");

  return (
    <div className="space-y-2 select-none">
      <div className="flex items-center justify-between gap-2 text-[12px] leading-none">
        <span className="font-medium text-[#8f98ac]">Ping Stats (24h)</span>
        <span className="shrink-0 font-mono text-[10px] font-semibold text-[#9aa3b7]">
          {pingStats.avgVolatility.toFixed(1)} Vol
        </span>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <CompactMetricBarStrip
          label="Latency"
          value={`${Math.round(pingStats.avgLatency)} ms`}
          points={latencyBars}
          metric="latency"
          t={t}
        />
        <CompactMetricBarStrip
          label="Loss"
          value={`${pingStats.avgLoss.toFixed(1)}%`}
          valueClassName={cn(
            pingStats.avgLoss > 5
              ? "text-[#e65a7a]"
              : pingStats.avgLoss > 1
                ? "text-[#e7a23a]"
                : "text-[#cfd6e6]"
          )}
          points={lossBars}
          metric="loss"
          t={t}
        />
      </div>
    </div>
  );
}

interface NodeProps {
  basic: NodeBasicInfo;
  live: Record | undefined;
  online: boolean;
}

const Node = ({ basic, live, online }: NodeProps) => {
  const [t] = useTranslation();
  const pingStats = usePingStats(basic.uuid, 24);

  const defaultLive = {
    cpu: { usage: 0 },
    ram: { used: 0 },
    disk: { used: 0 },
    network: { up: 0, down: 0, totalUp: 0, totalDown: 0 },
    uptime: 0,
  } as Record;

  const liveData = live || defaultLive;
  const memoryUsagePercent = basic.mem_total
    ? (liveData.ram.used / basic.mem_total) * 100
    : 0;
  const diskUsagePercent = basic.disk_total
    ? (liveData.disk.used / basic.disk_total) * 100
    : 0;

  const uploadSpeed = formatBytes(liveData.network.up);
  const downloadSpeed = formatBytes(liveData.network.down);
  const totalUpload = formatBytes(liveData.network.totalUp);
  const totalDownload = formatBytes(liveData.network.totalDown);
  const trafficLimitType = basic.traffic_limit_type ?? "sum";
  const trafficUsed = getTrafficUsed(
    liveData.network.totalUp,
    liveData.network.totalDown,
    trafficLimitType
  );
  const trafficPercentage = getTrafficPercentage(
    liveData.network.totalUp,
    liveData.network.totalDown,
    basic.traffic_limit,
    trafficLimitType
  );

  return (
    <div
      id={basic.uuid}
      className={`group relative flex min-h-[404px] w-full overflow-hidden rounded-[14px] border p-4 transition-all duration-300 ${
        !online
          ? "border-red-500/30 bg-[linear-gradient(135deg,rgba(20,26,42,0.95)_0%,rgba(14,19,32,0.98)_50%,rgba(10,14,26,0.99)_100%)] grayscale-[0.4] brightness-75 hover:grayscale-0 hover:brightness-100 hover:border-red-500/70 shadow-[inset_0_1px_0_rgba(255,255,255,0.04),0_8px_24px_rgba(0,0,0,0.4),0_2px_8px_rgba(239,68,68,0.08)] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_12px_40px_rgba(239,68,68,0.15),0_4px_16px_rgba(239,68,68,0.12)] hover:translate-y-[-2px]"
          : "border-[#3a4a66]/60 bg-[linear-gradient(135deg,rgba(20,26,42,0.95)_0%,rgba(14,19,32,0.98)_50%,rgba(10,14,26,0.99)_100%)] shadow-[inset_0_1px_0_rgba(255,255,255,0.06),0_8px_24px_rgba(0,0,0,0.4),0_2px_8px_rgba(94,109,255,0.08)] hover:border-[#5e6dff]/70 hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_12px_40px_rgba(94,109,255,0.15),0_4px_16px_rgba(94,109,255,0.12)] hover:translate-y-[-2px]"
      }`}
    >
      {!online && (
        <>
          <div className="absolute inset-0 z-10 bg-gradient-to-br from-black/70 via-black/60 to-red-950/40 backdrop-blur-[2px] flex items-center justify-center group-hover:opacity-0 group-hover:pointer-events-none transition-all duration-300">
            <div
              className="absolute inset-0 opacity-[0.03]"
              style={{
                backgroundImage: "radial-gradient(circle at 2px 2px, rgba(239,68,68,0.4) 1px, transparent 0)",
                backgroundSize: "24px 24px",
              }}
            />
            <div className="relative z-20 flex items-center gap-2 px-4 py-2.5 rounded-full bg-gradient-to-r from-red-500/25 via-red-500/20 to-red-600/25 border border-red-500/50 backdrop-blur-md shadow-[0_0_20px_rgba(239,68,68,0.3),inset_0_1px_0_rgba(255,255,255,0.1)]">
              <div className="relative">
                <div className="w-2 h-2 rounded-full bg-red-500 animate-pulse shadow-[0_0_8px_rgba(239,68,68,0.8)]" />
                <div className="absolute inset-0 w-2 h-2 rounded-full bg-red-400 animate-ping" />
              </div>
              <span className="text-red-300 font-bold text-sm tracking-wider drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]">
                {t("nodeCard.offline")}
              </span>
            </div>
          </div>
          <div className="absolute top-2 right-2 z-10 opacity-0 group-hover:opacity-100 transition-all duration-300 flex items-center gap-0.5 px-1.5 py-0.5 rounded-full bg-gradient-to-r from-red-500/30 to-red-600/30 border border-red-500/60 backdrop-blur-md shadow-lg">
            <div className="relative">
              <div className="w-0.5 h-0.5 rounded-full bg-red-400 animate-pulse shadow-[0_0_4px_rgba(239,68,68,0.9)]" />
            </div>
            <span className="text-red-300 font-bold text-[8px] tracking-wide leading-none drop-shadow-sm">
              {t("nodeCard.offline")}
            </span>
          </div>
        </>
      )}
      <div className="relative flex min-h-0 w-full flex-col">
        <div className="flex min-h-[56px] justify-between items-center">
          <div className="flex flex-1 min-w-0 items-center gap-3 overflow-hidden">
            <div className="flex-shrink-0">
              <Flag flag={basic.region} />
            </div>
            <div className="flex flex-col flex-1 min-w-0">
              <div className="flex flex-row min-w-0 items-center gap-1.5">
                <a
                  href={`#/instance/${basic.uuid}`}
                  className="group-hover:text-primary transition-all duration-200 overflow-hidden flex-1 min-w-0 hover:drop-shadow-[0_0_8px_rgba(94,109,255,0.6)]"
                >
                  <h3 className="font-bold truncate tracking-tight text-base">
                    {basic.name}
                  </h3>
                </a>
                {live?.message && <Tips color="#CE282E">{live.message}</Tips>}
                <LoadChartFloat
                  uuid={basic.uuid}
                  trigger={
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-6 w-6 text-muted-foreground hover:text-primary hover:bg-primary/10 flex-shrink-0 transition-all duration-200"
                    >
                      <TrendingUp className="h-3.5 w-3.5" />
                    </Button>
                  }
                />
              </div>
              <div className="flex items-center text-[11px] text-muted-foreground/80 gap-2 mt-0.5">
                <span className="flex items-center gap-1.5 bg-gradient-to-r from-[#1e2a3f]/60 to-[#1a2535]/60 border border-[#2a3a52]/40 px-2 py-1 rounded-md backdrop-blur-sm">
                  <img src={getOSImage(basic.os)} alt={basic.os} className="w-3 h-3 flex-shrink-0" />
                  <span className="whitespace-nowrap font-medium">{getOSName(basic.os)}</span>
                </span>
                <span
                  className={cn(
                    "w-1.5 h-1.5 rounded-full flex-shrink-0 shadow-[0_0_6px_currentColor] animate-pulse",
                    online ? "bg-green-500" : "bg-red-500"
                  )}
                  title={online ? t("nodeCard.online") : t("nodeCard.offline")}
                />
                <span className="flex-1" />
                <span className="opacity-40">•</span>
                <span className="whitespace-nowrap font-mono text-[#8a93a8]">
                  {formatUptimeCompact(liveData.uptime)}
                </span>
              </div>
            </div>
          </div>
        </div>

        <div className="-mx-4 mt-3 h-px bg-gradient-to-r from-transparent via-[#3a4a66]/40 to-transparent" />
        <div className="h-4" />

        <div className="grid grid-cols-3 items-start justify-items-center gap-3">
          <CircleChart
            value={liveData.cpu.usage}
            label="CPU"
            subLabel={`${liveData.cpu.usage.toFixed(1)}%`}
            compact
            size={76}
          />
          <CircleChart
            value={memoryUsagePercent}
            label="RAM"
            subLabel={formatBytes(liveData.ram.used)}
            compact
            size={76}
          />
          <CircleChart
            value={diskUsagePercent}
            label="Disk"
            subLabel={formatBytes(liveData.disk.used)}
            compact
            size={76}
          />
        </div>

        <div className="h-4" />

        <div className="space-y-2.5 text-[13px] select-none">
          <div className="flex min-w-0 items-center justify-between gap-4 px-3 py-2 rounded-lg bg-gradient-to-r from-[#1a2332]/40 to-[#141b2a]/40 border border-[#2a3a52]/30">
            <span className="inline-flex items-center gap-2 font-semibold text-[#a6aec1]">
              <Activity className="h-3.5 w-3.5 text-[#8a93a8]" />
              Net
            </span>
            <div className="flex min-w-0 items-center gap-4 font-mono text-[12px] font-extrabold tabular-nums">
              <span className="whitespace-nowrap text-[#00df7c] drop-shadow-[0_0_8px_rgba(0,223,124,0.4)]">
                ↑ {uploadSpeed}/s
              </span>
              <span className="whitespace-nowrap text-[#5ca9ff] drop-shadow-[0_0_8px_rgba(92,169,255,0.4)]">
                ↓ {downloadSpeed}/s
              </span>
            </div>
          </div>
          <div className="flex min-w-0 items-center justify-between gap-4 px-3 py-2 rounded-lg bg-gradient-to-r from-[#1a2332]/30 to-[#141b2a]/30 border border-[#2a3a52]/20">
            <span className="inline-flex items-center gap-2 font-semibold text-[#a6aec1]">
              <Activity className="h-3.5 w-3.5 text-[#8a93a8]" />
              Traffic
            </span>
            <div className="flex min-w-0 items-center gap-4 font-mono text-[12px] font-semibold text-[#aeb6c9] tabular-nums">
              <span className="whitespace-nowrap">↑ {totalUpload}</span>
              <span className="whitespace-nowrap">↓ {totalDownload}</span>
            </div>
          </div>
        </div>

        <div className="h-4" />

        <CompactPingTimeline pingStats={pingStats} t={t} />

        {basic.traffic_limit > 0 && (
          <div className="mt-2.5 space-y-1.5 select-none">
            <div className="flex items-center justify-between gap-2 text-[11px] leading-none text-[#9aa3b7]">
              <span className="font-medium tracking-tight">{trafficLimitType.toUpperCase()} Limit</span>
              <span className="shrink-0 font-mono font-semibold text-[#aeb6c9]">
                {formatBytes(trafficUsed)} / {formatBytes(basic.traffic_limit)}
              </span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-[#202838]/85">
              <div
                className={cn(
                  "h-full rounded-full",
                  trafficPercentage >= 90
                    ? "bg-[#d95473]/80"
                    : trafficPercentage >= 75
                      ? "bg-[#d59a25]/80"
                      : "bg-[#6572ff]/75"
                )}
                style={{ width: `${Math.min(trafficPercentage, 100)}%` }}
              />
            </div>
            <div className="flex justify-end font-mono text-[10px] font-semibold leading-none text-[#9aa3b7]">
              <span>{formatTrafficPercentage(trafficPercentage)}</span>
            </div>
          </div>
        )}

        <div className="mt-auto pt-2.5">
          <div className="pt-3">
            {(basic.price || basic.ipv4 || basic.ipv6) && (
              <PriceTags
                price={basic.price}
                billing_cycle={basic.billing_cycle}
                expired_at={basic.expired_at}
                currency={basic.currency}
                tags={basic.tags}
                ip4={basic.ipv4}
                ip6={basic.ipv6}
                compact={true}
              />
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Node;

type NodeGridProps = {
  nodes: NodeBasicInfo[];
  liveData: LiveData;
};

export const NodeGrid = ({ nodes, liveData }: NodeGridProps) => {
  const onlineNodes = liveData && liveData.online ? liveData.online : [];

  const sortedNodes = [...nodes].sort((a, b) => {
    const aOnline = onlineNodes.includes(a.uuid);
    const bOnline = onlineNodes.includes(b.uuid);

    if (aOnline !== bOnline) {
      return aOnline ? -1 : 1;
    }

    return a.weight - b.weight;
  });

  return (
    <div
      className="grid box-border w-full gap-4 py-3"
      style={{
        gridTemplateColumns: "repeat(auto-fill, minmax(284px, 1fr))",
      }}
    >
      {sortedNodes.map((node) => {
        const isOnline = onlineNodes.includes(node.uuid);
        const nodeData =
          liveData && liveData.data ? liveData.data[node.uuid] : undefined;

        return (
          <Node
            key={node.uuid}
            basic={node}
            live={nodeData}
            online={isOnline}
          />
        );
      })}
    </div>
  );
};
