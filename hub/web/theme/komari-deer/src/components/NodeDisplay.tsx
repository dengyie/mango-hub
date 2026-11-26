import React, { useState, useMemo, useRef, useEffect, Suspense } from "react";
import { Search, Grid3X3, Table2, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useLocalStorage } from "@/hooks/useLocalStorage";
import { useNodeViewMode } from "@/hooks/useNodeViewMode";
import { usePingTasks } from "@/hooks/usePingTasks";
import type { NodeBasicInfo } from "@/contexts/NodeListContext";
import type { LiveData } from "@/types/LiveData";
import { aggregateHealth, getNodeHealth, matchesHealthFilter, type HealthFilter, type NodeStatus } from "@/lib/dashboard";
import { emptyPingStats, summarizeLivePing } from "@/lib/ping";
import { NodeGrid, type DashboardNode } from "./Node";
import { matchesNodeSearch } from "@/lib/nodeProjects";
import { isRegionMatch } from "@/utils/regionHelper";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import "./NodeDisplay.css";
const NodeTable = React.lazy(() => import("./NodeTable"));

interface NodeDisplayProps { nodes: NodeBasicInfo[]; liveData?: LiveData | null; isLive: boolean; children?: React.ReactNode }
const FILTERS: HealthFilter[] = ["all", "attention", "offline", "cpu", "ram", "disk", "loss", "unknown"];

const NodeDisplay = ({ nodes, liveData, isLive, children }: NodeDisplayProps) => {
  const { t } = useTranslation();
  const [viewMode, setViewMode] = useNodeViewMode();
  const [searchTerm, setSearchTerm] = useState("");
  const [selectedGroup, setSelectedGroup] = useLocalStorage<string>("nodeSelectedGroup", "all");
  const [filter, setFilter] = useState<HealthFilter>("all");
  const searchRef = useRef<HTMLInputElement>(null);
  const metadata = usePingTasks();
  const sortedNodes = useMemo(() => [...nodes].sort((a, b) => a.weight - b.weight), [nodes]);
  const groups = useMemo(() => [...new Set(nodes.map((node) => node.group).filter(Boolean))].sort(), [nodes]);
  const group = selectedGroup === "all" || groups.includes(selectedGroup) ? selectedGroup : "all";
  const summaries = useMemo(() => {
    const online = new Set(liveData?.online);
    const result = new Map<string, DashboardNode>();
    for (const node of nodes) {
      const live = liveData?.data[node.uuid];
      let status: NodeStatus = "unknown";
      if (isLive) status = online.has(node.uuid) ? "online" : "offline";
      const ping = status === "online" ? summarizeLivePing(live?.ping ?? {}, metadata.types) : emptyPingStats();
      result.set(node.uuid, { health: getNodeHealth(node, live, status, ping), ping });
    }
    return result;
  }, [nodes, liveData, isLive, metadata.types]);
  const totals = useMemo(() => aggregateHealth(Array.from(summaries.values(), (item) => item.health)), [summaries]);
  const filteredNodes = useMemo(() => {
    return sortedNodes.filter((node) => {
      const health = summaries.get(node.uuid)!.health;
      if (group !== "all" && node.group !== group) return false;
      if (!matchesHealthFilter(health, filter)) return false;
      return matchesNodeSearch(node, searchTerm, health.status, isRegionMatch);
    });
  }, [sortedNodes, summaries, group, filter, searchTerm]);

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      const target = event.target;
      const typing = target instanceof HTMLElement && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));
      if (event.key === "/" && !typing && !event.ctrlKey && !event.metaKey && !event.altKey) { event.preventDefault(); searchRef.current?.focus(); }
      if (event.key === "Escape" && target === searchRef.current) { setSearchTerm(""); searchRef.current?.blur(); }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, []);
  const reset = () => { setSearchTerm(""); setFilter("all"); setSelectedGroup("all"); };

  let content: React.ReactNode;
  if (filteredNodes.length === 0) {
    content = <div className="rounded-xl border border-dashed bg-card py-12 text-center">
      <p className="mb-3 text-sm text-muted-foreground">{t("search.no_results")}</p>
      <Button variant="outline" onClick={reset}>{t("dashboard.reset")}</Button>
    </div>;
  } else if (viewMode === "grid") {
    content = <NodeGrid nodes={filteredNodes} liveData={liveData} summaries={summaries} />;
  } else {
    content = <Suspense fallback={<p className="p-4 text-sm">{t("dashboard.loading")}</p>}>
      <NodeTable nodes={filteredNodes} liveData={liveData} isLive={isLive} summaries={summaries} />
    </Suspense>;
  }

  return <div className="min-w-0 space-y-4">
    {children}
    <div className="flex flex-wrap items-baseline justify-between gap-2">
      <h1 className="text-xl font-semibold tracking-tight">{t("dashboard.overview")}</h1>
      <span className="text-xs text-muted-foreground">{t(isLive ? "dashboard.live" : "dashboard.stale")}</span>
    </div>
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4" aria-label={t("dashboard.overview")}>
      {[
        { key: "total", value: totals.total, hint: t("dashboard.inventory") },
        { key: "online", value: totals.online, hint: t("dashboard.offlineCount", { count: totals.offline }) },
        { key: "attention", value: totals.attention, hint: t("dashboard.attentionHint") },
        { key: "meanCpu", value: totals.meanCpu == null ? "—" : `${totals.meanCpu.toFixed(1)}%`, hint: t("dashboard.cpuHint") },
      ].map((item) => <div key={item.key} className="min-w-0 rounded-xl border bg-card px-4 py-3">
        <div className="text-xs text-muted-foreground">{t(`dashboard.${item.key}`)}</div>
        <div className={cn("mt-1 font-mono text-2xl font-semibold tabular-nums", item.key === "attention" && totals.attention > 0 && "text-amber-600 dark:text-amber-400")}>{item.value}</div>
        <div className="mt-1 text-[11px] text-muted-foreground">{item.hint}</div>
      </div>)}
    </div>
    {metadata.error && <div role="alert" className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-2 text-xs">
      <span>{t("dashboard.metadataError")}</span><Button variant="outline" size="sm" onClick={metadata.retry} disabled={metadata.loading}>{t("dashboard.retry")}</Button>
    </div>}
    <div className="flex flex-wrap items-center gap-3">
      <div className="relative min-w-0 flex-1 basis-full sm:basis-48">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input ref={searchRef} aria-label={t("dashboard.search")} placeholder={t("search.placeholder")} value={searchTerm} onChange={(event) => setSearchTerm(event.target.value)} className="h-10 bg-card pl-9 pr-10" />
        {searchTerm && <Button variant="ghost" size="icon" className="absolute right-0 top-0 h-10 w-10" aria-label={t("dashboard.clearSearch")} onClick={() => { setSearchTerm(""); searchRef.current?.focus(); }}><X className="h-4 w-4" /></Button>}
      </div>
      {groups.length > 0 && <label className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
        <span>{t("dashboard.group")}</span>
        <select aria-label={t("dashboard.group")} className="h-10 max-w-36 rounded-md border bg-card px-2 text-sm text-foreground" value={group} onChange={(event) => setSelectedGroup(event.target.value)}>
          <option value="all">{t("common.all")}</option>{groups.map((item) => <option key={item} value={item}>{item}</option>)}
        </select>
      </label>}
      <div className="flex shrink-0 rounded-lg border bg-card p-0.5" role="group" aria-label={t("dashboard.view")}>
        {(["grid", "table"] as const).map((mode) => <Button key={mode} variant={viewMode === mode ? "secondary" : "ghost"} size="icon" className="h-9 w-10" aria-label={t(`nodeDisplay.${mode}`)} aria-pressed={viewMode === mode} onClick={() => setViewMode(mode)}>{mode === "grid" ? <Grid3X3 className="h-4 w-4" /> : <Table2 className="h-4 w-4" />}</Button>)}
      </div>
    </div>
    <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label={t("dashboard.filters")}>
      <select aria-label={t("dashboard.filters")} value={filter} onChange={(event) => setFilter(event.target.value as HealthFilter)} className="h-10 max-w-[70%] rounded-md border bg-card px-3 text-sm sm:hidden">
        {FILTERS.map((item) => <option key={item} value={item}>{t(`dashboard.filters_${item}`)}</option>)}
      </select>
      <div className="hidden flex-wrap gap-1.5 sm:flex">
      {FILTERS.map((item) => <Button key={item} size="sm" variant={filter === item ? "secondary" : "ghost"} className="h-9 rounded-full px-3 text-xs" aria-pressed={filter === item} onClick={() => setFilter(item)}>{t(`dashboard.filters_${item}`)}</Button>)}
      </div>
      <span className="ml-auto text-xs text-muted-foreground" aria-live="polite">{t("dashboard.results", { count: filteredNodes.length, total: nodes.length })}</span>
    </div>
    {content}
  </div>;
};
export default NodeDisplay;
