import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, TrendingUp } from "lucide-react";
import type { TFunction } from "i18next";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { NodeBasicInfo } from "@/contexts/NodeListContext";
import type { LiveData, Record as LiveRecord } from "@/types/LiveData";
import type { NodeHealth } from "@/lib/dashboard";
import type { PingStats } from "@/lib/ping";
import { usePingStats } from "@/hooks/usePingStats";
import { getOSImage, getOSName } from "@/utils";
import { formatBytes } from "@/utils/unitHelper";
import { filterCnbReachability, isCnbProxyHost } from "@/utils/cnb";
import { miningStatusPill } from "@/utils/miningHelper";
import Flag from "./Flag";
import PriceTags from "./PriceTags";
import LoadChartFloat from "./LoadChartFloat";
import { NodeProjects } from "./NodeProjects";
import { parseNodeProjects } from "@/lib/nodeProjects";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
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

function ResourceMeter({ label, value }: { label: string; value: number | null }) {
  return <div className="min-w-0 space-y-2">
    <div className="flex items-baseline justify-between gap-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className={cn("font-mono font-semibold tabular-nums", value != null && value >= 90 && "text-amber-600 dark:text-amber-400")}>
        {value == null ? "—" : `${Math.round(value)}%`}
      </span>
    </div>
    <div className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden="true">
      {value != null && <div className={cn("h-full rounded-full", value >= 90 ? "bg-amber-500" : "bg-primary/70")} style={{ width: `${Math.min(value, 100)}%` }} />}
    </div>
  </div>;
}

function ProbeSummary({ stats }: { stats: PingStats }) {
  const { t } = useTranslation();
  return <div className="flex flex-wrap justify-between gap-x-3 gap-y-1 text-xs text-muted-foreground" title={t("dashboard.networkHint")}>
    <span>{t("dashboard.latency")} <strong className="font-mono font-medium text-foreground">{stats.avgLatency == null ? "—" : `${Math.round(stats.avgLatency)} ms`}</strong></span>
    <span>{t("dashboard.loss")} <strong className={cn("font-mono font-medium text-foreground", stats.avgLoss != null && stats.avgLoss > 5 && "text-rose-600 dark:text-rose-400")}>{stats.avgLoss == null ? "—" : `${stats.avgLoss.toFixed(1)}%`}</strong></span>
  </div>;
}

export function NodePingDetails({ basic }: { basic: NodeBasicInfo }) {
  const { t } = useTranslation();
  const { stats, loading, error, retry } = usePingStats(basic.uuid, true);
  const http = filterCnbReachability(stats.httpReachability, isCnbProxyHost(basic));
  return <section className="space-y-3" aria-label={t("dashboard.probes")} aria-busy={loading}>
    <div className="flex items-center justify-between gap-2">
      <h4 className="text-xs font-semibold">{t("dashboard.probes")}</h4>
      <Button variant="ghost" size="sm" className="min-h-9 px-2" onClick={retry} disabled={loading}>{t("dashboard.refresh")}</Button>
    </div>
    {loading && <p role="status" className="text-xs text-muted-foreground">{t("dashboard.loading")}</p>}
    {error && <p role="alert" className="text-xs text-destructive">{t("dashboard.historyError")}</p>}
    {!loading && !error && <>
      <ProbeSummary stats={stats} />
      {!stats.hasData && <p className="text-xs text-muted-foreground">{t("dashboard.noProbeData")}</p>}
      {stats.history.length > 0 && <div className="space-y-2">
        {(["latency", "loss"] as const).map((metric) => <div key={metric}>
          <div className="mb-1 text-[11px] text-muted-foreground">{t(`dashboard.${metric}`)}</div>
          <div className="grid h-3 grid-cols-[repeat(24,minmax(0,1fr))] gap-0.5" aria-label={t("dashboard.timelineHint")}>
            {stats.history.map((point) => {
              const value = point[metric];
              const high = value != null && value > (metric === "loss" ? 5 : 180);
              return <span key={point.time} className={cn("rounded-sm", value == null && "border border-border bg-muted", value != null && (high ? "bg-rose-500" : "bg-emerald-500/80"))}
                title={`${new Date(point.time).toLocaleTimeString()} · ${value == null ? t("dashboard.noData") : value.toFixed(1) + (metric === "loss" ? "%" : " ms")}`} />;
            })}
          </div>
        </div>)}
        <p className="text-[11px] text-muted-foreground">{t("dashboard.timelineHint")}</p>
      </div>}
      {http.length > 0 && <div className="space-y-2 border-t pt-3">
        <p className="text-xs text-muted-foreground">{t("dashboard.httpAvailability")}</p>
        {http.map((item, index) => <div key={`${item.name}-${index}`} className="flex items-start justify-between gap-2 text-xs">
          <span className="min-w-0 break-words">{item.name}</span>
          <span className="shrink-0 text-right">
            <strong className={cn("font-mono", item.availability != null && item.availability < 95 && "text-rose-600 dark:text-rose-400", item.availability != null && item.availability >= 99.9 && "text-emerald-600 dark:text-emerald-400")}>
              {item.availability == null ? "—" : `${item.availability.toFixed(2)}%`}
            </strong>
            <span className="block text-[10px] text-muted-foreground">{t("dashboard.lastSample")}: {item.up == null ? "—" : t(item.up ? "dashboard.reachable" : "dashboard.unreachable")}</span>
          </span>
        </div>)}
      </div>}
    </>}
  </section>;
}

interface NodeProps { basic: NodeBasicInfo; live: LiveRecord | undefined; health: NodeHealth; ping: PingStats }
const Node = ({ basic, live, health, ping }: NodeProps) => {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const current = health.status === "online";
  const mining = current ? live?.mining : undefined;
  const miningPill = miningStatusPill(mining);
  const network = current && live?.validMetrics?.network !== false ? live?.network : undefined;
  const trafficType = basic.traffic_limit_type ?? "sum";
  const projects = parseNodeProjects(basic.public_remark);
  const used = network ? getTrafficUsed(network.totalUp, network.totalDown, trafficType) : null;
  const percentage = network ? getTrafficPercentage(network.totalUp, network.totalDown, basic.traffic_limit, trafficType) : 0;
  const reportTime = live?.updated_at ? Date.parse(live.updated_at) : NaN;
  return <article id={basic.uuid} data-node-card className={cn("min-w-0 rounded-xl border bg-card p-4 text-card-foreground", health.status === "offline" && "border-rose-500/40")}>
    <div className="flex h-[228px] flex-col">
      <div className="flex items-start gap-2.5">
        <div className="shrink-0 pt-1"><Flag flag={basic.region} /></div>
        <div className="min-w-0 flex-1">
          <a href={`#/instance/${basic.uuid}`} className="block truncate rounded-sm text-sm font-semibold hover:text-primary focus-visible:outline-2 focus-visible:outline-primary" title={basic.name}>{basic.name}</a>
          <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <img src={getOSImage(basic.os)} alt="" width={14} height={14} className="h-3.5 w-3.5 shrink-0 object-contain" />
            <span className="truncate">{getOSName(basic.os)}</span>
            {live?.message && <Tips color="#CE282E">{live.message}</Tips>}
            <StatusIndicator
              status={current ? "online" : health.status === "unknown" ? "connecting" : "offline"}
              label={t(current ? "nodeCard.online" : `dashboard.${health.status}`)}
              className="ml-auto shrink-0 px-2 py-0.5 text-[11px] leading-none"
            />
          </div>
        </div>
        <LoadChartFloat uuid={basic.uuid} trigger={<Button variant="ghost" size="icon" className="-mr-1 -mt-1 h-9 w-9 shrink-0" aria-label={t("dashboard.charts", { name: basic.name })}><TrendingUp className="h-4 w-4" /></Button>} />
      </div>
      <div className="my-4 grid grid-cols-3 gap-4">
        <ResourceMeter label="CPU" value={health.cpu} /><ResourceMeter label="RAM" value={health.ram} /><ResourceMeter label={t("nodeCard.disk")} value={health.disk} />
      </div>
      <ProbeSummary stats={ping} />
      <p className="mt-2 h-4 truncate text-[11px] leading-4 text-muted-foreground" title={health.unknown ? t(current ? "dashboard.noProbeData" : "dashboard.staleNode") : undefined}>
        {health.unknown ? t(current ? "dashboard.noProbeData" : "dashboard.staleNode") : !current && t("dashboard.offline")}
      </p>
      <NodeProjects projects={projects} compact maxVisible={2} className="mt-1.5" />
      <Button variant="ghost" className="mt-auto h-9 w-full shrink-0 justify-between px-0 text-xs text-muted-foreground hover:bg-transparent hover:text-foreground" aria-expanded={expanded} aria-controls={`details-${basic.uuid}`} onClick={() => setExpanded((value) => !value)}>
        <span>{t(expanded ? "dashboard.hideDetails" : "dashboard.details")}</span><ChevronDown className={cn("h-4 w-4 transition-transform", expanded && "rotate-180")} />
      </Button>
    </div>
    {expanded && <div id={`details-${basic.uuid}`} className="mt-1 space-y-3 border-t pt-3">
      {!current && <p className="text-[11px] text-muted-foreground">{t("dashboard.lastReport")}: {Number.isFinite(reportTime) ? <time dateTime={live?.updated_at}>{new Date(reportTime).toLocaleString()}</time> : "—"}</p>}
      <dl className="space-y-2 text-xs">
        <div className="flex justify-between gap-2"><dt className="text-muted-foreground">{t("nodeCard.uptime")}</dt><dd>{current && live ? formatUptimeCompact(live.uptime) : "—"}</dd></div>
        <div className="flex flex-wrap justify-between gap-2"><dt className="text-muted-foreground">{t("nodeCard.networkSpeed")}</dt><dd className="font-mono">{network ? `↑ ${formatBytes(network.up)}/s · ↓ ${formatBytes(network.down)}/s` : "—"}</dd></div>
        <div className="flex flex-wrap justify-between gap-2"><dt className="text-muted-foreground">{t("nodeCard.totalTransfer")}</dt><dd className="font-mono">{network ? `↑ ${formatBytes(network.totalUp)} · ↓ ${formatBytes(network.totalDown)}` : "—"}</dd></div>
      </dl>
      {basic.traffic_limit > 0 && <div className="space-y-1 text-xs">
        <div className="flex justify-between gap-2 text-muted-foreground"><span>{t("dashboard.trafficLimit")} ({trafficType.toUpperCase()})</span><span>{used == null ? "—" : formatTrafficPercentage(percentage)}</span></div>
        <div className="h-1.5 overflow-hidden rounded-full bg-muted"><div className="h-full bg-primary/70" style={{ width: `${Math.min(percentage, 100)}%` }} /></div>
        <p className="text-right font-mono text-muted-foreground">{used == null ? "—" : formatBytes(used)} / {formatBytes(basic.traffic_limit)}</p>
      </div>}
      {miningPill && <div className="flex justify-between gap-2 rounded-md bg-muted/50 p-2 text-xs"><span>{miningPill.name}</span><span className={cn("font-mono", miningPill.up ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>{miningPill.metric}</span></div>}
      {projects.length > 0 && <div className="border-t pt-3"><NodeProjects projects={projects} showLabel /></div>}
      <NodePingDetails basic={basic} />
      <div className="border-t pt-3"><PriceTags price={basic.price} billing_cycle={basic.billing_cycle} expired_at={basic.expired_at} currency={basic.currency} tags={basic.tags} ip4={basic.ipv4} ip6={basic.ipv6} compact /></div>
    </div>}
  </article>;
};
export default Node;

export interface DashboardNode { health: NodeHealth; ping: PingStats }
export const NodeGrid = ({ nodes, liveData, summaries }: { nodes: NodeBasicInfo[]; liveData?: LiveData | null; summaries: ReadonlyMap<string, DashboardNode> }) => {
  const available: NodeBasicInfo[] = [], offline: NodeBasicInfo[] = [];
  for (const node of nodes) (summaries.get(node.uuid)?.health.status === "offline" ? offline : available).push(node);
  return <div className="grid w-full grid-cols-[repeat(auto-fill,minmax(min(100%,280px),1fr))] items-start gap-4">
    {[...available, ...offline].map((node) => {
      const summary = summaries.get(node.uuid);
      return summary && <Node key={node.uuid} basic={node} live={liveData?.data[node.uuid]} {...summary} />;
    })}
  </div>;
};
