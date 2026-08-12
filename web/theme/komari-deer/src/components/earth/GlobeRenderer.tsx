"use client";

import { useCallback, useEffect, useMemo, useRef } from "react";
import Globe from "globe.gl";
import * as THREE from "three";
import { getEarthCountryPolygons } from "./earthCountryPolygons";
import type { EarthArcData, EarthPointData } from "./earthData";
import { getEarthStructuralSignature, hasEarthStructuralChange } from "./globeDataSignature";

type EarthBackgroundConfig = {
  bg: string;
  bgImage: string | null;
  globeImage: string;
  bumpImage: string;
  atmosphere: string;
};

type GlobeRendererProps = {
  pointsData: EarthPointData[];
  arcsData: EarthArcData[];
  bgConfig: EarthBackgroundConfig;
  themeColor: string;
  userLat: number;
  userLng: number;
  logoUrl?: string;
  logoShape?: "circle" | "original";
  onReady?: () => void;
};

function escapeHtml(value: string) {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

function calculateFlagSize(altitude: number): number {
  const baseSize = 24;
  const minSize = 16;
  const maxSize = 40;
  const zoomFactor = Math.max(0.5, Math.min(2, 3 - altitude));
  return Math.max(minSize, Math.min(maxSize, baseSize * zoomFactor));
}

function hasOverlap(pointsData: EarthPointData[]) {
  const serverPoints = pointsData.filter((point) => point.type === "server");

  for (let i = 0; i < serverPoints.length; i += 1) {
    for (let j = i + 1; j < serverPoints.length; j += 1) {
      const latDiff = Math.abs(serverPoints[i].lat - serverPoints[j].lat);
      const lngDiff = Math.abs(serverPoints[i].lng - serverPoints[j].lng);
      const distance = Math.sqrt(latDiff * latDiff + lngDiff * lngDiff);
      if (distance < 2) return true;
    }
  }

  return false;
}

export default function GlobeRenderer({
  pointsData,
  arcsData,
  bgConfig,
  themeColor,
  userLat,
  userLng,
  logoUrl = "/assets/logo.png",
  logoShape = "circle",
  onReady,
}: GlobeRendererProps) {
  const stageRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLDivElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const globeRef = useRef<any>(null);
  const activeMarkerRef = useRef<HTMLElement | null>(null);
  const themeColorRef = useRef(themeColor);
  const bgConfigRef = useRef(bgConfig);
  const dataSignatureRef = useRef("");
  const countryPolygons = useMemo(
    () =>
      getEarthCountryPolygons(
        pointsData
          .filter((point) => point.type === "server")
          .map((point) => point.code),
      ),
    [pointsData],
  );
  const polygonFillMaterials = useMemo(
    () => ({
      country: new THREE.MeshBasicMaterial({
        color: 0x00d8ff,
        transparent: true,
        opacity: 0.22,
        depthWrite: false,
        side: THREE.DoubleSide,
      }),
      synthetic: new THREE.MeshBasicMaterial({
        color: 0x00ffff,
        transparent: true,
        opacity: 0.28,
        depthWrite: false,
        side: THREE.DoubleSide,
      }),
    }),
    [],
  );

  const hideTooltip = useCallback(() => {
    const tooltip = tooltipRef.current;
    if (tooltip) {
      tooltip.style.display = "none";
      tooltip.style.transform = "";
      tooltip.textContent = "";
    }

    const activeMarker = activeMarkerRef.current;
    if (activeMarker) {
      activeMarker.style.transform = "translate(-50%, -50%) scale(1)";
      activeMarker.style.zIndex = "auto";
      activeMarker.style.filter = `drop-shadow(0 0 4px ${themeColorRef.current})`;
    }

    activeMarkerRef.current = null;

    if (globeRef.current) {
      globeRef.current.controls().autoRotate = true;
    }
  }, []);

  const showTooltip = useCallback(
    (marker: HTMLElement, point: EarthPointData) => {
      const tooltip = tooltipRef.current;
      const canvas = canvasRef.current;
      if (!tooltip || !canvas) return;

      const safeCode = escapeHtml(point.code);
      const safeServers = point.servers.map((server) => escapeHtml(server));
      const statusLabel =
        point.type === "user"
          ? "Location"
          : `${point.online} online / ${point.offline} offline`;

      tooltip.style.display = "block";
      tooltip.innerHTML = `
        <div class="deer-earth-tooltip-header">
          <span>${safeCode}</span>
          <span>${escapeHtml(statusLabel)}</span>
        </div>
        <div class="deer-earth-tooltip-content">
          ${safeServers.map((server) => `<div class="deer-earth-tooltip-item">${server}</div>`).join("")}
        </div>
      `;

      const parentRect = canvas.getBoundingClientRect();
      const markerRect = marker.getBoundingClientRect();
      const tooltipRect = tooltip.getBoundingClientRect();

      if (window.innerWidth <= 768) {
        tooltip.style.left = "50%";
        tooltip.style.top = "auto";
        tooltip.style.right = "auto";
        tooltip.style.bottom = "20px";
        tooltip.style.transform = "translateX(-50%)";
        return;
      }

      let left = markerRect.right - parentRect.left + 15;
      let top = markerRect.top - parentRect.top;

      if (left + tooltipRect.width > parentRect.width) {
        left = markerRect.left - parentRect.left - tooltipRect.width - 15;
      }

      if (top + tooltipRect.height > parentRect.height) {
        top = parentRect.height - tooltipRect.height - 10;
      }

      tooltip.style.left = `${Math.max(10, left)}px`;
      tooltip.style.top = `${Math.max(10, top)}px`;
      tooltip.style.right = "auto";
      tooltip.style.bottom = "auto";
      tooltip.style.transform = "";
    },
    []
  );

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || globeRef.current) return;

    const overlap = hasOverlap(pointsData);
    const initialAltitude = window.innerWidth <= 768 ? 3 : 2;
    const globe = new (Globe as any)(canvas)
      .width(canvas.clientWidth)
      .height(canvas.clientHeight)
      .backgroundColor(bgConfig.bg)
      .backgroundImageUrl(bgConfig.bgImage)
      .globeImageUrl(bgConfig.globeImage)
      .bumpImageUrl(bgConfig.bumpImage)
      .atmosphereColor(bgConfig.atmosphere)
      .atmosphereAltitude(0.16)
      .htmlElementsData(pointsData)
      .htmlLat((point: EarthPointData) => point.lat)
      .htmlLng((point: EarthPointData) => point.lng)
      .htmlElement((point: EarthPointData) => {
        const wrapper = document.createElement("div");

        if (point.type === "user") {
          const marker = document.createElement("button");
          marker.type = "button";
          marker.className = `deer-earth-user-marker${logoShape === "original" ? " is-original" : ""}`;
          marker.setAttribute("aria-label", "Your location");

          if (logoUrl) {
            const img = document.createElement("img");
            img.src = logoUrl;
            img.alt = "";
            marker.appendChild(img);
          } else {
            marker.textContent = "YOU";
          }

          marker.onclick = (event) => {
            event.stopPropagation();
            if (activeMarkerRef.current === marker) {
              hideTooltip();
              return;
            }
            hideTooltip();
            activeMarkerRef.current = marker;
            globe.controls().autoRotate = false;
            showTooltip(marker, point);
          };

          wrapper.appendChild(marker);
          return wrapper;
        }

        const img = document.createElement("img");
        img.src = `/assets/flags/${point.code}.svg`;
        img.alt = point.code;
        img.className = `deer-earth-flag-img status-${point.status}`;
        img.style.pointerEvents = "auto";

        const updateFlagSize = () => {
          const controls = globe.controls();
          const altitude = controls.getDistance ? controls.getDistance() / 200 : 2;
          const nextSize = overlap && altitude < 1.5
            ? Math.max(16, calculateFlagSize(altitude) * 0.7)
            : calculateFlagSize(altitude);
          img.style.width = `${nextSize}px`;
        };

        updateFlagSize();
        globe.controls().addEventListener("change", updateFlagSize);

        img.onmouseenter = () => {
          if (activeMarkerRef.current !== img) {
            img.style.transform = "translate(-50%, -50%) scale(1.3)";
            img.style.filter = `drop-shadow(0 0 8px ${themeColorRef.current})`;
          }
        };
        img.onmouseleave = () => {
          if (activeMarkerRef.current !== img) {
            img.style.transform = "translate(-50%, -50%) scale(1)";
            img.style.filter = `drop-shadow(0 0 4px ${themeColorRef.current})`;
          }
        };
        img.onclick = (event) => {
          event.stopPropagation();
          if (activeMarkerRef.current === img) {
            hideTooltip();
            return;
          }
          hideTooltip();
          activeMarkerRef.current = img;
          globe.controls().autoRotate = false;
          img.style.transform = "translate(-50%, -50%) scale(1.5)";
          img.style.zIndex = "1000";
          img.style.filter = `drop-shadow(0 0 10px ${themeColorRef.current})`;
          showTooltip(img, point);
        };

        wrapper.appendChild(img);
        return wrapper;
      })
      .polygonsData(countryPolygons)
      .polygonAltitude((polygon: { synthetic?: boolean }) => (polygon.synthetic ? 0.008 : 0.004))
      .polygonCapMaterial((polygon: { synthetic?: boolean }) =>
        polygon.synthetic ? polygonFillMaterials.synthetic : polygonFillMaterials.country
      )
      .polygonStrokeColor(() => "rgba(0, 255, 255, 0.92)")
      .ringsData(pointsData.filter((point) => point.type === "server"))
      .ringColor(() => themeColorRef.current)
      .ringMaxRadius(2)
      .ringPropagationSpeed(1)
      .ringRepeatPeriod(1250)
      .arcsData(arcsData)
      .arcStartLat((arc: EarthArcData) => arc.startLat)
      .arcStartLng((arc: EarthArcData) => arc.startLng)
      .arcEndLat((arc: EarthArcData) => arc.endLat)
      .arcEndLng((arc: EarthArcData) => arc.endLng)
      .arcColor(() => themeColorRef.current)
      .arcDashLength(0.5)
      .arcDashGap(1)
      .arcDashAnimateTime(1750)
      .arcStroke(0.8)
      .arcAltitudeAutoScale(0.5);

    globe.pointOfView({ lat: userLat || 25, lng: userLng || 110, altitude: initialAltitude });
    globe.controls().autoRotate = true;
    globe.controls().autoRotateSpeed = 0.6;
    globe.controls().enableZoom = true;

    const handleCanvasClick = (event: Event) => {
      const target = event.target as HTMLElement;
      if (target === canvas || target.tagName === "CANVAS") {
        hideTooltip();
      }
    };
    const handleResize = () => {
      globe.width(canvas.clientWidth);
      globe.height(canvas.clientHeight);
    };

    canvas.addEventListener("click", handleCanvasClick);
    window.addEventListener("resize", handleResize);

    let resizeObserver: ResizeObserver | null = null;
    if ("ResizeObserver" in window) {
      resizeObserver = new ResizeObserver(handleResize);
      resizeObserver.observe(canvas);
    }

    globeRef.current = globe;
    bgConfigRef.current = { ...bgConfig };
    dataSignatureRef.current = getEarthStructuralSignature(pointsData, arcsData);
    onReady?.();

    return () => {
      canvas.removeEventListener("click", handleCanvasClick);
      window.removeEventListener("resize", handleResize);
      resizeObserver?.disconnect();
      polygonFillMaterials.country.dispose();
      polygonFillMaterials.synthetic.dispose();
      globeRef.current = null;
    };
    // Globe owns DOM nodes, so this initializes once and later effects update data/config.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    themeColorRef.current = themeColor;
    const globe = globeRef.current;
    if (!globe) return;
    globe.arcColor(() => themeColor);
    globe.ringColor(() => themeColor);
  }, [themeColor]);

  useEffect(() => {
    const globe = globeRef.current;
    if (!globe) return;

    if (!hasEarthStructuralChange(dataSignatureRef.current, pointsData, arcsData)) return;

    if (activeMarkerRef.current) {
      hideTooltip();
    }

    globe.htmlElementsData(pointsData);
    globe.polygonsData(countryPolygons);
    globe.ringsData(pointsData.filter((point) => point.type === "server"));
    globe.arcsData(arcsData);

    dataSignatureRef.current = getEarthStructuralSignature(pointsData, arcsData);
  }, [pointsData, arcsData, countryPolygons, hideTooltip]);

  useEffect(() => {
    const globe = globeRef.current;
    if (!globe) return;

    const previous = bgConfigRef.current;
    if (
      previous.bg === bgConfig.bg &&
      previous.bgImage === bgConfig.bgImage &&
      previous.globeImage === bgConfig.globeImage &&
      previous.bumpImage === bgConfig.bumpImage &&
      previous.atmosphere === bgConfig.atmosphere
    ) {
      return;
    }

    globe.backgroundColor(bgConfig.bg);
    globe.backgroundImageUrl(bgConfig.bgImage);
    globe.globeImageUrl(bgConfig.globeImage);
    globe.bumpImageUrl(bgConfig.bumpImage);
    globe.atmosphereColor(bgConfig.atmosphere);
    bgConfigRef.current = { ...bgConfig };
  }, [bgConfig]);

  return (
    <div ref={stageRef} className="deer-earth-stage">
      <div ref={canvasRef} className="deer-earth-canvas" />
      <div ref={tooltipRef} className="deer-earth-tooltip" />
    </div>
  );
}
