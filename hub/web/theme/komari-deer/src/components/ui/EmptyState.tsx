import React from "react";
import { UiIcon, UiIconName } from "./UiIcon";

export interface EmptyStateProps {
  icon?: UiIconName;
  title: string;
  description?: string;
  actionLabel?: string;
  onAction?: () => void;
  className?: string;
}

export const EmptyState: React.FC<EmptyStateProps> = ({
  icon = "sparkles",
  title,
  description,
  actionLabel,
  onAction,
  className = "",
}) => {
  return (
    <div className={`flex flex-col items-center justify-center text-center px-6 py-12 ${className}`}>
      <div className="p-3 rounded-2xl bg-zinc-100 dark:bg-zinc-800 text-zinc-400 dark:text-zinc-500 mb-4">
        <UiIcon name={icon} size={24} />
      </div>
      <div className="text-sm font-semibold text-zinc-800 dark:text-zinc-200">{title}</div>
      {description && <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500 max-w-xs">{description}</div>}
      {actionLabel && (
        <button
          type="button"
          onClick={onAction}
          className="mt-4 px-3.5 py-1.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-medium transition-colors"
        >
          {actionLabel}
        </button>
      )}
    </div>
  );
};
export default EmptyState;
