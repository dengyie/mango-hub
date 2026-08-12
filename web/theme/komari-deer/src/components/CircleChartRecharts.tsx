"use client";

import { RadialBarChart, RadialBar, PolarAngleAxis, ResponsiveContainer } from "recharts";

interface CircleChartRechartsProps {
  value: number; // clamped 0-100
  label: string;
  subLabel?: string;
  compact?: boolean; // Compact mode for table views
  size?: number; // Optional custom pixel size
  visualSize?: number; // Optional visual-only ring size for compact charts
  fillColor: string;
}

export default function CircleChartRecharts({
  value,
  label,
  subLabel,
  compact = false,
  size,
  visualSize,
  fillColor,
}: CircleChartRechartsProps) {
  const data = [
    {
      name: label,
      value,
      fill: fillColor,
    },
  ];

  // Compact mode with visual size for table views
  if (compact) {
    const compactSize = size ?? 40;
    const compactVisualSize = visualSize ?? compactSize;
    const compactBarSize = visualSize ? 5 : 7;

    return (
      <div className="flex items-center justify-center">
        <div className="relative overflow-visible" style={{ height: compactSize, width: compactSize }}>
          <div
            className="absolute left-1/2 top-1/2"
            style={{
              height: compactVisualSize,
              width: compactVisualSize,
              transform: "translate(-50%, -50%)",
            }}
          >
            <ResponsiveContainer width="100%" height="100%">
              <RadialBarChart
                cx="50%"
                cy="50%"
                innerRadius="65%"
                outerRadius="95%"
                barSize={compactBarSize}
                data={data}
                startAngle={90}
                endAngle={-270}
              >
                <PolarAngleAxis
                  type="number"
                  domain={[0, 100]}
                  angleAxisId={0}
                  tick={false}
                />
                <RadialBar
                  background={{ fill: 'rgba(128, 128, 128, 0.1)' }}
                  dataKey="value"
                  cornerRadius={10}
                  animationDuration={800}
                  animationEasing="ease-out"
                />
              </RadialBarChart>
            </ResponsiveContainer>
          </div>

          {/* Centered Percentage for compact mode */}
          <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
            <span className="text-[11px] font-bold text-foreground">
              {Math.round(value)}%
            </span>
          </div>
        </div>
      </div>
    );
  }

  // Default mode with labels
  const chartSize = size ?? 90;

  return (
    <div className="flex flex-col items-center justify-center p-2">
      <div className="relative" style={{ height: chartSize, width: chartSize }}>
        <ResponsiveContainer width="100%" height="100%">
          <RadialBarChart
            cx="50%"
            cy="50%"
            innerRadius="70%"
            outerRadius="95%"
            barSize={8}
            data={data}
            startAngle={90}
            endAngle={-270}
          >
            <PolarAngleAxis
              type="number"
              domain={[0, 100]}
              angleAxisId={0}
              tick={false}
            />
            <RadialBar
              background={{ fill: 'rgba(128, 128, 128, 0.1)' }}
              dataKey="value"
              cornerRadius={10}
              animationDuration={800}
              animationEasing="ease-out"
            />
          </RadialBarChart>
        </ResponsiveContainer>

        {/* Centered Percentage */}
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
          <span className="text-base font-bold text-foreground drop-shadow-sm tracking-tight">
            {Math.round(value)}%
          </span>
        </div>
      </div>

      {/* Labels */}
      <div className="text-center mt-2">
        <div className="text-xs font-semibold text-foreground/90">{label}</div>
        {subLabel && (
          <div className="text-[10px] text-muted-foreground/60 mt-0.5">{subLabel}</div>
        )}
      </div>
    </div>
  );
}
