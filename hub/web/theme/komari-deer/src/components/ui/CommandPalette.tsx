import React, { useState, useEffect, useMemo, useRef } from "react";
import { UiIcon, UiIconName } from "./UiIcon";

export interface CommandItem {
  id: string;
  label: string;
  hint?: string;
  icon?: UiIconName;
  group?: string;
  shortcut?: string;
}

export interface CommandPaletteProps {
  items: CommandItem[];
  isOpen: boolean;
  onClose?: () => void;
  onSelect?: (item: CommandItem) => void;
  placeholder?: string;
  className?: string;
}

export const CommandPalette: React.FC<CommandPaletteProps> = ({
  items = [],
  isOpen,
  onClose,
  onSelect,
  placeholder = "Type a command or search...",
  className = "",
}) => {
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = useMemo(() => {
    if (!query.trim()) return items;
    const q = query.toLowerCase();
    return items.filter(
      (i) =>
        i.label.toLowerCase().includes(q) ||
        (i.hint && i.hint.toLowerCase().includes(q)) ||
        (i.group && i.group.toLowerCase().includes(q))
    );
  }, [items, query]);

  useEffect(() => {
    if (isOpen) {
      setQuery("");
      setActiveIndex(0);
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [isOpen]);

  useEffect(() => setActiveIndex(0), [query]);

  useEffect(() => {
    if (!isOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose?.();
      } else if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((i) => Math.min(filtered.length - 1, i + 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((i) => Math.max(0, i - 1));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const item = filtered[activeIndex];
        if (item) {
          onSelect?.(item);
          onClose?.();
        }
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [isOpen, filtered, activeIndex, onClose, onSelect]);

  if (!isOpen) return null;

  let lastGroup: string | undefined;

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh] bg-black/40 backdrop-blur-sm"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
    >
      <div
        className={`w-full max-w-lg mx-4 rounded-2xl border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150 ${className}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2.5 px-4 py-3 border-b border-zinc-100 dark:border-zinc-800">
          <UiIcon name="search" size={16} className="text-zinc-400 shrink-0" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={placeholder}
            className="w-full bg-transparent border-none text-sm text-zinc-800 dark:text-zinc-200 placeholder-zinc-400 focus:outline-none"
          />
          <kbd className="hidden sm:inline-block px-1.5 py-0.5 text-[10px] font-mono text-zinc-400 bg-zinc-100 dark:bg-zinc-800 rounded border border-zinc-200 dark:border-zinc-700">
            ESC
          </kbd>
        </div>

        <div className="max-h-72 overflow-y-auto p-1.5" role="listbox">
          {filtered.length === 0 ? (
            <div className="p-4 text-center text-sm text-zinc-400">No results found</div>
          ) : (
            filtered.map((item, idx) => {
              const showGroup = item.group && item.group !== lastGroup;
              lastGroup = item.group;
              const isActive = idx === activeIndex;
              return (
                <React.Fragment key={item.id}>
                  {showGroup && (
                    <div className="px-2.5 pt-2.5 pb-1 text-[10px] uppercase font-semibold tracking-wider text-zinc-400">
                      {item.group}
                    </div>
                  )}
                  <button
                    type="button"
                    role="option"
                    aria-selected={isActive}
                    onClick={() => {
                      onSelect?.(item);
                      onClose?.();
                    }}
                    onMouseEnter={() => setActiveIndex(idx)}
                    className={`w-full flex items-center gap-3 px-2.5 py-2 rounded-lg text-left text-sm transition-colors ${
                      isActive
                        ? "bg-zinc-100 dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100"
                        : "text-zinc-700 dark:text-zinc-300"
                    }`}
                  >
                    {item.icon && (
                      <span className="p-1 rounded-md bg-zinc-200/60 dark:bg-zinc-700/60 text-zinc-500 dark:text-zinc-400 shrink-0">
                        <UiIcon name={item.icon} size={14} />
                      </span>
                    )}
                    <span className="flex-1 min-w-0">
                      <span className="block truncate font-medium">{item.label}</span>
                      {item.hint && (
                        <span className="block truncate text-[11px] text-zinc-400">{item.hint}</span>
                      )}
                    </span>
                    {item.shortcut && (
                      <kbd className="px-1.5 py-0.5 text-[10px] font-mono text-zinc-400 bg-zinc-100 dark:bg-zinc-800 rounded border border-zinc-200 dark:border-zinc-700 shrink-0">
                        {item.shortcut}
                      </kbd>
                    )}
                  </button>
                </React.Fragment>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
};
export default CommandPalette;
