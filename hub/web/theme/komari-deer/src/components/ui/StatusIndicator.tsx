import React from "react";

export type StatusType = "online" | "offline" | "busy" | "connecting" | "error";

export interface StatusIndicatorProps {
  status: StatusType;
  label?: string;
  pingMs?: number;
  showDot?: boolean;
  className?: string;
}

export const StatusIndicator: React.FC<StatusIndicatorProps> = ({
  status,
  label,
  pingMs,
  showDot = true,
  className = "",
}) => {
  const getStatusConfig = () => {
    switch (status) {
      case "online":
        return {
          dotBg: "bg-emerald-500",
          pingBg: "bg-emerald-400",
          text: "text-emerald-700 dark:text-emerald-400",
          badgeBg: "bg-emerald-50 dark:bg-emerald-950/40 border-emerald-200/80 dark:border-emerald-800/60",
          defaultLabel: "Online",
        };
      case "connecting":
      case "busy":
        return {
          dotBg: "bg-amber-500",
          pingBg: "bg-amber-400",
          text: "text-amber-700 dark:text-amber-400",
          badgeBg: "bg-amber-50 dark:bg-amber-950/40 border-amber-200/80 dark:border-amber-800/60",
          defaultLabel: status === "busy" ? "Busy" : "Connecting",
        };
      case "error":
      case "offline":
      default:
        return {
          dotBg: "bg-rose-500",
          pingBg: "bg-rose-400",
          text: "text-rose-700 dark:text-rose-400",
          badgeBg: "bg-rose-50 dark:bg-rose-950/40 border-rose-200/80 dark:border-rose-800/60",
          defaultLabel: status === "error" ? "Error" : "Offline",
        };
    }
  };

  const config = getStatusConfig();
  const displayLabel = label || config.defaultLabel;

  return (
    <div
      className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border text-xs font-medium ${config.badgeBg} ${config.text} ${className}`}
    >
      {showDot && (
        <span className="relative flex h-2 w-2">
          {status === "online" && (
            <span
              className={`animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 ${config.pingBg}`}
            />
          )}
          <span className={`relative inline-flex rounded-full h-2 w-2 ${config.dotBg}`} />
        </span>
      )}
      <span className="font-semibold">{displayLabel}</span>
      {pingMs !== undefined && (
        <span className="font-mono text-[10px] opacity-75 ml-0.5">
          ({pingMs}ms)
        </span>
      )}
    </div>
  );
};
