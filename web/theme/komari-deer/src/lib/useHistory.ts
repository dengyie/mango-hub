"use client";

import { useEffect, useSyncExternalStore } from "react";
import {
  subscribe,
  getVersion,
  getEntry,
  loadHistory,
  SWR_TTL,
} from "./historyCache";
import type { RecordsData } from "./historyCache";

export type HistoryStatus = "idle" | "loading" | "ready" | "stale" | "error";

export function useHistory(uuid: string, hours?: number): {
  data: RecordsData | null;
  status: HistoryStatus;
  error: string | null;
} {
  useSyncExternalStore(subscribe, getVersion, () => 0); // 第三参 = getServerSnapshot，避免 SSR/hydration 快照不一致

  useEffect(() => {
    void loadHistory(uuid, hours, { force: true }); // 打开图表/切视图 → 展示缓存 + 无条件后台重拉
  }, [uuid, hours]);

  if (!uuid || !hours) return { data: null, status: "idle", error: null }; // 实时视图/无 uuid：不拉历史、不显示 loading

  const entry = getEntry(uuid, hours);
  const hasData = entry?.data != null;
  const hasError = !!entry?.error;
  const inFlight = !!entry?.promise;
  const stale =
    hasData && entry.fetchedAt !== null && Date.now() - entry.fetchedAt >= SWR_TTL;

  let status: HistoryStatus = "loading";
  if (hasData) status = stale ? "stale" : "ready";
  else if (hasError && !inFlight) status = "error"; // 有错且无重试在飞 → 错误态；重试中算 loading
  else status = "loading";

  return { data: entry?.data ?? null, status, error: hasError ? entry.error : null };
}
