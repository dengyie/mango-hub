"use client";

import React, { Suspense } from "react";
import dynamic from "next/dynamic";
import { useTheme } from "@/contexts/ThemeContext";

const CircleChartRecharts = dynamic(() => import("@/components/CircleChartRecharts"), {
  ssr: false,
});

interface CircleChartProps {
  value: number; // 0-100
  label: string;
  subLabel?: string;
  color?: string; // Optional override
  compact?: boolean; // Compact mode for table views
  size?: number; // Optional custom pixel size
  visualSize?: number; // Optional visual-only ring size for compact charts
}

export default function CircleChart({
  value,
  label,
  subLabel,
  color,
  compact = false,
  size,
  visualSize,
}: CircleChartProps) {
  const { themeConfig } = useTheme();

  // Clamp value
  const chartValue = Math.min(Math.max(value, 0), 100);

  // Get theme color based on selected color theme and value
  const getThemeColor = () => {
    if (color) return color; // Use override if provided

    const getColorForTheme = () => {
      switch (themeConfig.colorTheme) {
        case 'ocean':
          return chartValue >= 80 ? '#0284c7' : chartValue >= 60 ? '#06b6d4' : '#22d3ee';
        case 'sunset':
          return chartValue >= 80 ? '#ec4899' : chartValue >= 60 ? '#f97316' : '#fb923c';
        case 'forest':
          return chartValue >= 80 ? '#059669' : chartValue >= 60 ? '#10b981' : '#4ade80';
        case 'midnight':
          return chartValue >= 80 ? '#7c3aed' : chartValue >= 60 ? '#6366f1' : '#818cf8';
        case 'rose':
          return chartValue >= 80 ? '#e11d48' : chartValue >= 60 ? '#ec4899' : '#f472b6';
        default: // 'default'
          return chartValue >= 80 ? '#9333ea' : chartValue >= 60 ? '#3b82f6' : '#60a5fa';
      }
    };

    return getColorForTheme();
  };

  const fillColor = getThemeColor();

  if (compact && visualSize == null) {
    const chartSize = size ?? 58;
    const radius = 18.5;
    const strokeWidth = 4.4;
    const circumference = 2 * Math.PI * radius;
    const strokeDashoffset = circumference - (chartValue / 100) * circumference;
    const compactFillColor =
      color ?? (chartValue >= 80 ? "#7c3aed" : chartValue >= 60 ? "#6366f1" : "#818cf8");

    return (
      <div className="flex flex-col items-center justify-center select-none">
        <div className="relative" style={{ width: chartSize, height: chartSize }}>
          <svg
            width={chartSize}
            height={chartSize}
            viewBox="0 0 50 50"
            className="h-full w-full -rotate-90 transform"
          >
            <circle
              cx="25"
              cy="25"
              r={radius}
              stroke="rgba(128, 128, 128, 0.1)"
              strokeWidth={strokeWidth}
              fill="transparent"
            />
            <circle
              cx="25"
              cy="25"
              r={radius}
              stroke={compactFillColor}
              strokeWidth={strokeWidth}
              fill="transparent"
              strokeDasharray={circumference}
              strokeDashoffset={strokeDashoffset}
              strokeLinecap="round"
              className="transition-all duration-500 ease-out"
            />
          </svg>

          <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
            <span
              className="font-extrabold leading-none tracking-tight"
              style={{ fontSize: "16px", color: "#f4f6ff" }}
            >
              {Math.round(chartValue)}%
            </span>
          </div>
        </div>

        <div className="mt-1 text-center">
          <span className="mt-1.5 block text-[12px] font-semibold tracking-tight text-[#aeb6c9]">
            {label}
          </span>
          {subLabel && (
            <span className="mt-0.5 block text-[11px] text-[#626b7e]">
              {subLabel}
            </span>
          )}
        </div>
      </div>
    );
  }

  // Default mode and compact-with-visualSize use recharts — lazy-loaded off the critical path
  const chartSize = compact ? (visualSize ?? size ?? 40) : (size ?? 90);
  return (
    <Suspense
      fallback={
        compact ? (
          <div
            className="flex animate-pulse items-center justify-center"
            style={{ height: chartSize, width: chartSize }}
          >
            <div
              className="rounded-full bg-white/[0.06]"
              style={{ height: chartSize, width: chartSize }}
            />
          </div>
        ) : (
          <div className="flex animate-pulse flex-col items-center justify-center p-2">
            <div
              className="rounded-full bg-white/[0.06]"
              style={{ height: chartSize, width: chartSize }}
            />
            <div className="mt-2 h-3 w-16 rounded bg-white/[0.06]" />
            <div className="mt-1 h-2 w-10 rounded bg-white/[0.04]" />
          </div>
        )
      }
    >
      <CircleChartRecharts
        value={chartValue}
        label={label}
        subLabel={subLabel}
        compact={compact}
        size={size}
        visualSize={visualSize}
        fillColor={fillColor}
      />
    </Suspense>
  );
}
