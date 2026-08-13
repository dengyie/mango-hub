"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, PointerEvent } from "react";
import { MapPinned } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { NodeBasicInfo } from "@/contexts/NodeListContext";
import type { LiveData } from "@/types/LiveData";
import { buildMapViewSummary, type MapRegionSummary } from "@/utils/mapRegions";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import Flag from "@/components/Flag";
import type {
  NodeMapCountryGeometry,
  ProjectedWorldMap,
} from "@/components/nodeMapViewGeometry";
import {
  getRotatedMap,
  INITIAL_ROTATION,
} from "@/components/nodeMapViewGeometry";

import "./NodeMapView.css";

/** 自转经度增量(度/帧)。约 0.15°/帧 ≈ 45fps 下每秒 ~6.75°,缓慢可读。 */
const ROTATE_DEG_PER_FRAME = 0.15;
// 拖拽灵敏度:每像素对应的旋转度数。0.3°/px → 拖拽 ~1200px 转一圈,手感顺滑且不过快。
const DRAG_DEG_PER_PX = 0.3;
// 纬度 clamp:正射投影下 |phi| 超过 90° 会翻转,故限制在 ±60° 保持自然视角。
const MAX_LATITUDE_DEG = 60;

interface NodeMapViewProps {
  nodes: NodeBasicInfo[];
  liveData: LiveData;
  mapOnly?: boolean;
}

type TranslateFn = (key: string, options?: Record<string, unknown>) => string;
type HoveredRegion = {
  regionKey: string;
  x: number;
  y: number;
  horizontal: "left" | "right";
  vertical: "above" | "below";
};

const SVG_WIDTH = 1000;
const SVG_HEIGHT = 560;
const HOVER_CARD_GAP = 12;
const HOVER_CARD_MAX_WIDTH = 320;
const HOVER_CARD_FALLBACK_HEIGHT = 124;
const HOVER_CARD_EDGE_PADDING = 8;

function getStatusText(t: TranslateFn, status: "online" | "offline" | "partial") {
  switch (status) {
    case "online":
      return t("mapView.status.online", { defaultValue: "Online" });
    case "offline":
      return t("mapView.status.offline", { defaultValue: "Offline" });
    default:
      return t("mapView.status.partial", { defaultValue: "Partially online" });
  }
}

function getUnmappedRegionLabel(t: TranslateFn, region: string) {
  const normalizedRegion = region.trim();
  return normalizedRegion || t("mapView.regionUnknown", { defaultValue: "Not set" });
}

function getRegionStatusBadgeClass(status: "online" | "offline" | "partial") {
  switch (status) {
    case "online":
      return "bg-emerald-500/12 text-emerald-700 dark:bg-emerald-500/18 dark:text-emerald-300";
    case "offline":
      return "bg-rose-500/12 text-rose-700 dark:bg-rose-500/18 dark:text-rose-300";
    default:
      return "bg-amber-500/14 text-amber-700 dark:bg-amber-500/18 dark:text-amber-300";
  }
}

/**
 * 绑定到每个国家 path 的视图模型:几何(随旋转更新)+当前关联的区域摘要+是否高亮。
 * 几何 pathData 由 React 渲染初始帧(SSG 一致),rAF 自转循环里用 imperative setAttribute
 * 覆盖更新 path d 与 marker 坐标,避免每帧触发 React reconciliation(177 国)。
 */
type CountryView = NodeMapCountryGeometry & {
  activeRegion: MapRegionSummary | null;
  marker: { x: number; y: number } | null;
};

export function NodeMapView({
  nodes,
  liveData,
  mapOnly = false,
}: NodeMapViewProps) {
  const { t } = useTranslation();
  const summary = useMemo(() => buildMapViewSummary(nodes, liveData), [nodes, liveData]);
  const [hoveredRegion, setHoveredRegion] = useState<HoveredRegion | null>(null);
  const mapSurfaceRef = useRef<HTMLDivElement | null>(null);
  const hoverCardRef = useRef<HTMLDivElement | null>(null);
  const hoverFrameRef = useRef<number | null>(null);
  const pendingHoverPositionRef = useRef<Omit<HoveredRegion, "regionKey"> | null>(null);

  // 自转运行态(全部走 ref,不触发重渲染):rAF 句柄、当前 rotate、暂停标志、reduced-motion 偏好。
  const rafRef = useRef<number | null>(null);
  const rotationRef = useRef<[number, number, number]>(INITIAL_ROTATION);
  const pausedRef = useRef(false);
  const reducedMotionRef = useRef(false);
  // 拖拽态:active 标记 + pointerdown 时的起始坐标与起始 rotation。
  // 拖拽中不触发国家 hover(避免卡片抖动);pointerup 后恢复自转。
  const dragRef = useRef<{
    active: boolean;
    pointerId: number;
    startX: number;
    startY: number;
    startRotation: [number, number, number];
  }>({ active: false, pointerId: -1, startX: 0, startY: 0, startRotation: INITIAL_ROTATION });

  // surface 挂载信号:summary 为空时组件渲染空态卡片(无 surface),mapSurfaceRef 为 null,
  // 自转 effect 此时跑会因 querySelector 落空而 return 且永不重试(空依赖)。用此 state 在
  // surface 实际挂载(数据到位、渲染带地图的分支)后触发 effect 重跑,确保 rAF 启动。
  const [surfaceMounted, setSurfaceMounted] = useState(false);

  const hoverRegion =
    summary.regions.find((region) => region.key === hoveredRegion?.regionKey) ?? null;
  const hoverPosition = hoveredRegion
    ? pendingHoverPositionRef.current ?? hoveredRegion
    : null;

  const activeRegionsByMapName = useMemo(
    () => new Map(summary.regions.map((region) => [region.mapName, region])),
    [summary.regions],
  );

  // 初始帧几何(SSG 一致,无 JS 即纯静态,水合前与水合后一贯),后续旋转由 imperative 更新覆盖。
  const initialProjectedMap = useMemo<ProjectedWorldMap>(
    () => getRotatedMap(INITIAL_ROTATION),
    [],
  );

  // 国家视图模型:仅初始帧以 React 渲染,派生活跃状态用于 className/aria/hover 绑定。
  const initialCountriesView = useMemo<CountryView[]>(
    () =>
      initialProjectedMap.countries
        .map((country) => {
          const activeRegion = activeRegionsByMapName.get(country.name) ?? null;
          return {
            ...country,
            activeRegion,
            marker: activeRegion ? country.smallRegionMarker : null,
          };
        })
        .filter((country) => country.pathData),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  /**
   * 共用重投影+DOM 更新:按 rotationRef 当前角度重投影,imperative 写入 sphere/graticule/
   * 国家 path d/marker 坐标。自转 tick 与拖拽 move 均调用此函数,避免逻辑重复。
   * 不触发 React rerender(纯 DOM 属性写入)。
   */
  const applyRotation = useCallback(() => {
    const surface = mapSurfaceRef.current;
    const countryLayer = surface?.querySelector<SVGGElement>("[data-layer='countries']");
    const markerLayer = surface?.querySelector<SVGGElement>("[data-layer='markers']");
    if (!surface || !countryLayer || !markerLayer) {
      return;
    }
    const map = getRotatedMap(rotationRef.current);

    surface
      ?.querySelector<SVGPathElement>("[data-layer='sphere']")
      ?.setAttribute("d", map.spherePath);
    surface
      ?.querySelector<SVGPathElement>("[data-layer='graticule']")
      ?.setAttribute("d", map.graticulePath);

    const geoByName = new Map<string, (typeof map.countries)[number]>();
    for (const c of map.countries) {
      geoByName.set(c.name, c);
    }

    const pathEls = countryLayer.querySelectorAll<SVGPathElement>("path[data-cname]");
    const markerEls = markerLayer.querySelectorAll<SVGGElement>("g[data-cname]");
    for (const pathEl of pathEls) {
      const next = geoByName.get(pathEl.getAttribute("data-cname") ?? "");
      if (next) {
        pathEl.setAttribute("d", next.pathData);
      }
    }
    for (const markerEl of markerEls) {
      const next = geoByName.get(markerEl.getAttribute("data-cname") ?? "");
      const halo = markerEl.querySelector<SVGCircleElement>("circle[data-role='halo']");
      const dot = markerEl.querySelector<SVGCircleElement>("circle[data-role='dot']");
      if (next?.smallRegionMarker) {
        if (halo) {
          halo.setAttribute("cx", String(next.smallRegionMarker.x));
          halo.setAttribute("cy", String(next.smallRegionMarker.y));
        }
        if (dot) {
          dot.setAttribute("cx", String(next.smallRegionMarker.x));
          dot.setAttribute("cy", String(next.smallRegionMarker.y));
        }
      }
    }
  }, []);

  /**
   * 自转循环:每帧停步式更新投影 rotate,通过 applyRotation 把新 path d 写入已渲染的 DOM。
   * 整个循环不调用 setState,因此 177 path 不会进入 React reconciliation,水合与 hover 逻辑也不被干扰。
   */
  useEffect(() => {
    // 启动前同步读取 prefers-reduced-motion,避免依赖另一个 effect 的执行顺序。
    // 偏好减少动效时不启动自转循环(保持初始静态正射视角,但仍允许手动拖拽)。
    if (
      typeof window !== "undefined" &&
      window.matchMedia &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    ) {
      reducedMotionRef.current = true;
      return;
    }

    const surface = mapSurfaceRef.current;
    if (!surface?.querySelector("[data-layer='countries']")) {
      return;
    }

    let cancelled = false;

    const tick = () => {
      if (cancelled) {
        return;
      }
      if (!pausedRef.current && !dragRef.current.active) {
        const r = rotationRef.current;
        r[0] = (r[0] + ROTATE_DEG_PER_FRAME) % 360;
        applyRotation();
      }
      rafRef.current = window.requestAnimationFrame(tick);
    };

    rafRef.current = window.requestAnimationFrame(tick);

    return () => {
      cancelled = true;
      if (rafRef.current !== null) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [surfaceMounted, applyRotation]);

  // 尊重 prefers-reduced-motion:用户偏好减少动效时不启动自转。
  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) {
      return;
    }
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    const sync = () => {
      reducedMotionRef.current = mq.matches;
    };
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);

  const getHoverPosition = useCallback((event: PointerEvent<SVGElement>) => {
    const surfaceRect = mapSurfaceRef.current?.getBoundingClientRect();
    const boundsWidth = surfaceRect?.width ?? window.innerWidth;
    const boundsHeight = surfaceRect?.height ?? window.innerHeight;
    const x = surfaceRect ? event.clientX - surfaceRect.left : event.clientX;
    const y = surfaceRect ? event.clientY - surfaceRect.top : event.clientY;
    const hoverCard = hoverCardRef.current;
    const cardWidth =
      hoverCard?.offsetWidth ??
      Math.min(HOVER_CARD_MAX_WIDTH, Math.max(0, boundsWidth - HOVER_CARD_EDGE_PADDING * 2));
    const cardHeight = hoverCard?.offsetHeight ?? HOVER_CARD_FALLBACK_HEIGHT;
    const spaceRight = boundsWidth - x - HOVER_CARD_GAP;
    const spaceLeft = x - HOVER_CARD_GAP;
    const spaceBelow = boundsHeight - y - HOVER_CARD_GAP;
    const spaceAbove = y - HOVER_CARD_GAP;

    return {
      x,
      y,
      horizontal: spaceRight >= cardWidth || spaceRight >= spaceLeft ? "right" : "left",
      vertical: spaceBelow >= cardHeight || spaceBelow >= spaceAbove ? "below" : "above",
    } satisfies Omit<HoveredRegion, "regionKey">;
  }, []);

  const applyHoverPosition = useCallback((position: Omit<HoveredRegion, "regionKey">) => {
    const hoverCard = hoverCardRef.current;
    if (!hoverCard) {
      return;
    }

    hoverCard.style.setProperty("--node-map-hover-x", `${position.x}px`);
    hoverCard.style.setProperty("--node-map-hover-y", `${position.y}px`);
    hoverCard.dataset.horizontal = position.horizontal;
    hoverCard.dataset.vertical = position.vertical;
  }, []);

  const queueHoverPosition = useCallback(
    (position: Omit<HoveredRegion, "regionKey">) => {
      pendingHoverPositionRef.current = position;

      if (hoverFrameRef.current !== null) {
        return;
      }

      hoverFrameRef.current = window.requestAnimationFrame(() => {
        hoverFrameRef.current = null;
        const nextPosition = pendingHoverPositionRef.current;

        if (nextPosition) {
          applyHoverPosition(nextPosition);
        }
      });
    },
    [applyHoverPosition],
  );

  const updateHoveredRegion = useCallback(
    (event: PointerEvent<SVGElement>, region: MapRegionSummary) => {
      const position = getHoverPosition(event);

      setHoveredRegion({
        regionKey: region.key,
        ...position,
      });
      queueHoverPosition(position);
    },
    [getHoverPosition, queueHoverPosition],
  );

  const updateHoverPosition = useCallback(
    (event: PointerEvent<SVGElement>) => {
      queueHoverPosition(getHoverPosition(event));
    },
    [getHoverPosition, queueHoverPosition],
  );

  const clearHoveredRegion = useCallback(() => {
    setHoveredRegion(null);
    pendingHoverPositionRef.current = null;

    if (hoverFrameRef.current !== null) {
      window.cancelAnimationFrame(hoverFrameRef.current);
      hoverFrameRef.current = null;
    }
  }, []);

  useEffect(() => {
    return () => {
      if (hoverFrameRef.current !== null) {
        window.cancelAnimationFrame(hoverFrameRef.current);
      }
    };
  }, []);

  // 鼠标进入/离开 surface 暂停或恢复自转,便于用户阅读与点击具体国家;hover 具体国家同样暂停。
  const handleSurfacePointerEnter = useCallback(() => {
    if (!dragRef.current.active) {
      pausedRef.current = true;
    }
  }, []);
  const handleSurfacePointerLeave = useCallback(() => {
    if (!dragRef.current.active) {
      pausedRef.current = false;
      clearHoveredRegion();
    }
  }, [clearHoveredRegion]);

  // 拖拽转动地球:pointerdown 记录起点+当前 rotation 并暂停自转;move 按像素 delta 改 rotation
  // (水平→经度,垂直→纬度 clamp ±60°)并实时重投影;up 恢复自转。拖拽中禁用 hover 避免卡片抖动。
  // setPointerCapture 保证拖出元素外仍持续追踪 pointer。
  const handleSurfacePointerDown = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      const surface = mapSurfaceRef.current;
      if (!surface?.querySelector("[data-layer='countries']")) {
        return;
      }
      dragRef.current = {
        active: true,
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY,
        startRotation: [...rotationRef.current] as [number, number, number],
      };
      pausedRef.current = true;
      clearHoveredRegion();
      try {
        surface.setPointerCapture(event.pointerId);
      } catch {
        // 某些浏览器/环境下 setPointerCapture 可能抛异常,忽略不影响拖拽逻辑。
      }
    },
    [clearHoveredRegion],
  );

  const handleSurfacePointerMove = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      const drag = dragRef.current;
      if (!drag.active || event.pointerId !== drag.pointerId) {
        return;
      }
      const dx = event.clientX - drag.startX;
      const dy = event.clientY - drag.startY;
      const r = rotationRef.current;
      // 水平拖拽改经度(取模 360 避免数值膨胀);垂直拖拽改纬度并 clamp。
      r[0] = (drag.startRotation[0] - dx * DRAG_DEG_PER_PX) % 360;
      const lat = drag.startRotation[1] + dy * DRAG_DEG_PER_PX;
      r[1] = Math.max(-MAX_LATITUDE_DEG, Math.min(MAX_LATITUDE_DEG, lat));
      applyRotation();
    },
    [applyRotation],
  );

  const endDrag = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      const drag = dragRef.current;
      if (!drag.active || event.pointerId !== drag.pointerId) {
        return;
      }
      dragRef.current = {
        active: false,
        pointerId: -1,
        startX: 0,
        startY: 0,
        startRotation: rotationRef.current,
      };
      try {
        mapSurfaceRef.current?.releasePointerCapture(event.pointerId);
      } catch {
        // 忽略:元素可能已卸载或未捕获该 pointer。
      }
      // 恢复自转(reduced-motion 时不自转,但 pausedRef 复位以保持一致性)。
      pausedRef.current = false;
    },
    [],
  );

  if (!summary.totalNodes) {
    return (
      <Card className="overflow-hidden rounded-[28px] border-border/70 bg-card/95 shadow-sm">
        {!mapOnly && (
          <CardHeader>
            <CardTitle>{t("mapView.title", { defaultValue: "Global Distribution" })}</CardTitle>
          </CardHeader>
        )}
        <CardContent>
          <div className="rounded-3xl border border-dashed border-border/70 bg-muted/40 px-6 py-12 text-center text-sm text-muted-foreground">
            {t("nodes.empty", { defaultValue: "No node data" })}
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card
      className={
        mapOnly
          ? "node-map-view overflow-visible rounded-none border-0 bg-transparent shadow-none"
          : "node-map-view overflow-hidden rounded-[28px] border-border/70 bg-card/95 shadow-sm"
      }
    >
      {!mapOnly && (
        <CardHeader className="space-y-4 border-b border-border/70 pb-5">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="inline-flex items-center gap-2 rounded-full bg-sky-500/10 px-3 py-1 text-xs font-medium text-sky-700 dark:bg-sky-500/14 dark:text-sky-300">
                <MapPinned className="h-3.5 w-3.5" />
                {t("common.map", { defaultValue: "Map" })}
              </div>
              <CardTitle className="text-2xl tracking-tight">
                {t("mapView.title", { defaultValue: "Global Distribution" })}
              </CardTitle>
              <p className="text-sm text-muted-foreground">
                {t("mapView.activeCountries", {
                  count: summary.regions.length,
                  defaultValue: "{{count}} active countries / regions",
                })}
              </p>
            </div>

            <div className="flex flex-wrap gap-2">
              <Badge
                variant="secondary"
                className="rounded-full bg-muted px-3 py-1 text-muted-foreground"
              >
                {t("mapView.servers", {
                  count: summary.totalNodes,
                  defaultValue: "{{count}} servers",
                })}
              </Badge>
              <Badge
                variant="secondary"
                className="rounded-full bg-emerald-500/12 px-3 py-1 text-emerald-700 dark:bg-emerald-500/18 dark:text-emerald-300"
              >
                {t("mapView.online", {
                  count: summary.onlineNodes,
                  defaultValue: "{{count}} online",
                })}
              </Badge>
              <Badge
                variant="secondary"
                className="rounded-full bg-rose-500/12 px-3 py-1 text-rose-700 dark:bg-rose-500/18 dark:text-rose-300"
              >
                {t("mapView.offline", {
                  count: summary.offlineNodes,
                  defaultValue: "{{count}} offline",
                })}
              </Badge>
            </div>
          </div>
        </CardHeader>
      )}

      <CardContent className={mapOnly ? "p-0" : "p-5 lg:p-6"}>
        <div className={mapOnly ? "node-map-view__layout node-map-view__layout--map-only" : "node-map-view__layout"}>
          <div
            ref={(node) => {
              mapSurfaceRef.current = node;
              setSurfaceMounted(!!node);
            }}
            className="node-map-view__surface"
            data-map-spinning
            style={{ touchAction: "none" }}
            onPointerEnter={handleSurfacePointerEnter}
            onPointerLeave={handleSurfacePointerLeave}
            onPointerDown={handleSurfacePointerDown}
            onPointerMove={handleSurfacePointerMove}
            onPointerUp={endDrag}
            onPointerCancel={endDrag}
          >
            <svg
              viewBox={`0 0 ${SVG_WIDTH} ${SVG_HEIGHT}`}
              className="node-map-view__svg"
              role="img"
              aria-label={t("mapView.ariaLabel", { defaultValue: "Global node distribution map" })}
            >
              <defs>
                <radialGradient
                  id="node-map-view__ocean-gradient"
                  cx="42%"
                  cy="38%"
                  r="62%"
                >
                  <stop offset="0%" stopColor="rgba(226,240,253,0.96)" />
                  <stop offset="55%" stopColor="rgba(244,248,252,0.9)" />
                  <stop offset="92%" stopColor="rgba(203,221,237,0.86)" />
                  <stop offset="100%" stopColor="rgba(180,201,224,0.9)" />
                </radialGradient>
              </defs>
              <path data-layer="sphere" d={initialProjectedMap.spherePath} className="node-map-view__ocean" />
              <path data-layer="graticule" d={initialProjectedMap.graticulePath} className="node-map-view__graticule" />

              <g data-layer="countries" className="node-map-view__country-layer">
                {initialCountriesView.map((country) => {
                  const region = country.activeRegion;
                  const isSelected = hoveredRegion?.regionKey === region?.key;
                  const ariaLabel = region
                    ? t("mapView.countrySummary", {
                        name: region.label,
                        total: region.total,
                        online: region.online,
                        offline: region.offline,
                        defaultValue:
                          "{{name}}: {{total}} nodes, {{online}} online, {{offline}} offline",
                      })
                    : country.name;

                  return (
                    <g key={country.name} className="node-map-view__country-group">
                      <path
                        d={country.pathData}
                        data-cname={country.name}
                        data-country-code={region?.flagCode}
                        data-country-name={country.name}
                        className={`node-map-view__country${region ? ` is-active status-${region.status}` : ""}${isSelected ? " is-selected" : ""}`}
                        aria-label={ariaLabel}
                        onPointerEnter={region ? (event) => updateHoveredRegion(event, region) : undefined}
                        onPointerMove={region ? updateHoverPosition : undefined}
                        onPointerLeave={region ? clearHoveredRegion : undefined}
                      />
                    </g>
                  );
                })}
              </g>

              <g data-layer="markers" className="node-map-view__marker-layer">
                {initialCountriesView
                  .filter((country) => country.activeRegion && country.marker)
                  .map((country) => {
                    const region = country.activeRegion;
                    const marker = country.marker;
                    if (!region || !marker) {
                      return null;
                    }

                    const isSelected = hoveredRegion?.regionKey === region.key;
                    const ariaLabel = t("mapView.countrySummary", {
                      name: region.label,
                      total: region.total,
                      online: region.online,
                      offline: region.offline,
                      defaultValue:
                        "{{name}}: {{total}} nodes, {{online}} online, {{offline}} offline",
                    });

                    return (
                      <g
                        key={`${country.name}-marker`}
                        className={`node-map-view__marker status-${region.status}${isSelected ? " is-selected" : ""}`}
                        data-cname={country.name}
                        data-country-code={region.flagCode}
                        data-country-name={country.name}
                        aria-label={ariaLabel}
                        onPointerEnter={(event) => updateHoveredRegion(event, region)}
                        onPointerMove={updateHoverPosition}
                        onPointerLeave={clearHoveredRegion}
                      >
                        <circle
                          cx={marker.x}
                          cy={marker.y}
                          r="9"
                          data-role="halo"
                          className="node-map-view__marker-halo"
                        />
                        <circle
                          cx={marker.x}
                          cy={marker.y}
                          r="4.2"
                          data-role="dot"
                          className="node-map-view__marker-dot"
                        />
                      </g>
                    );
                  })}
              </g>
            </svg>

            <div className="node-map-view__legend node-map-view__legend--inset">
              <div className="node-map-view__legend-card node-map-view__legend-card--status">
                <div className="node-map-view__legend-items node-map-view__legend-items--stacked">
                  <span className="node-map-view__legend-item">
                    <span className="node-map-view__legend-dot status-online" />
                    {t("mapView.legend.online", { defaultValue: "Fully online" })}
                  </span>
                  <span className="node-map-view__legend-item">
                    <span className="node-map-view__legend-dot status-partial" />
                    {t("mapView.legend.partial", { defaultValue: "Partially online" })}
                  </span>
                  <span className="node-map-view__legend-item">
                    <span className="node-map-view__legend-dot status-offline" />
                    {t("mapView.legend.offline", { defaultValue: "Fully offline" })}
                  </span>
                </div>
              </div>

              {summary.unmappedNodes.length > 0 && (
                <div className="node-map-view__legend-card node-map-view__legend-card--stacked">
                  <div className="node-map-view__legend-unmapped-header">
                    <span className="text-xs font-semibold text-foreground">
                      {t("mapView.unmappedRegions", { defaultValue: "Unmapped Regions" })}
                    </span>
                    <Badge
                      variant="secondary"
                      className="rounded-full bg-amber-500/12 px-2.5 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-500/18 dark:text-amber-300"
                    >
                      {t("mapView.unmappedCount", {
                        count: summary.unmappedNodes.length,
                        defaultValue: "Total {{count}} items",
                      })}
                    </Badge>
                  </div>
                  <div className="node-map-view__legend-unmapped-list">
                    {summary.unmappedNodes.map((node) => (
                      <div key={`${node.uuid}-unmapped`} className="node-map-view__legend-unmapped-item">
                        <span className="node-map-view__legend-unmapped-region">
                          {getUnmappedRegionLabel(t, node.region)}
                        </span>
                        <span className="node-map-view__legend-unmapped-node">{node.name}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>

            {hoverRegion && hoverPosition && (
              <div
                ref={hoverCardRef}
                className="node-map-view__hover-card"
                data-horizontal={hoverPosition.horizontal}
                data-vertical={hoverPosition.vertical}
                style={{
                  "--node-map-hover-x": `${hoverPosition.x}px`,
                  "--node-map-hover-y": `${hoverPosition.y}px`,
                } as CSSProperties}
              >
                <div className="node-map-view__detail-header node-map-view__hover-header">
                  <div className="node-map-view__detail-heading">
                    <span className="node-map-view__detail-flag" aria-hidden="true">
                      <Flag flag={hoverRegion.emoji} />
                    </span>
                    <div className="min-w-0 space-y-1">
                      <div className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                        {hoverRegion.flagCode}
                      </div>
                      <h3 className="truncate text-lg font-semibold tracking-tight text-foreground">
                        {hoverRegion.label}
                      </h3>
                      <div className="node-map-view__hover-count-line">
                        <span className="node-map-view__hover-count-total">
                          {hoverRegion.total}
                          <span>{t("mapView.stats.nodes", { defaultValue: "Nodes" })}</span>
                        </span>
                        <span className="node-map-view__hover-status-counts">
                          <span className="node-map-view__hover-count node-map-view__hover-count--online">
                            {hoverRegion.online} {t("nodeCard.online", { defaultValue: "Online" })}
                          </span>
                          <span className="node-map-view__hover-count node-map-view__hover-count--offline">
                            {hoverRegion.offline} {t("nodeCard.offline", { defaultValue: "Offline" })}
                          </span>
                        </span>
                      </div>
                    </div>
                  </div>

                  <Badge
                    variant="secondary"
                    className={`shrink-0 whitespace-nowrap rounded-full ${getRegionStatusBadgeClass(hoverRegion.status)}`}
                  >
                    {getStatusText(t, hoverRegion.status)}
                  </Badge>
                </div>
              </div>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
