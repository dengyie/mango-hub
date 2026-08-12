"use client";

import React, { Suspense, useEffect } from "react";
import { useTranslation } from "react-i18next";

import NodeDisplay from "@/components/NodeDisplay";
import { useLiveData } from "@/contexts/LiveDataContext";
import { useNodeList } from "@/contexts/NodeListContext";
import { usePublicInfo } from "@/contexts/PublicInfoContext";
import { preloadAll } from "@/lib/historyCache";
import DashboardSkeleton from "@/components/DashboardSkeleton";
import { Callouts } from "@/components/DashboardCallouts";
import { NodeMapView } from "@/components/NodeMapView";

const MemoNodeMapView = React.memo(NodeMapView);
const MemoNodeDisplay = React.memo(NodeDisplay);

export default function DashboardContent() {
  const [t] = useTranslation();
  const { live_data } = useLiveData();
  const { publicInfo } = usePublicInfo();
  
  // Sync document title with backend-set custom title
  useEffect(() => {
    if (publicInfo?.sitename) {
      document.title = publicInfo.sitename;
    }
  }, [publicInfo?.sitename]);
  
  //#region 节点数据
  const { nodeList, isLoading, error, refresh } = useNodeList();

  useEffect(() => {
    if (!nodeList?.length) return;
    const uuids = nodeList.map((n) => n.uuid);
    // 首屏关键资源优先：等浏览器空闲再启动历史预载（5×114KB），避免与首帧/分块加载竞争带宽；弹窗打开仍会 force 重拉
    const start = () => void preloadAll(uuids, 24);
    if (typeof requestIdleCallback === "function") {
      const id = requestIdleCallback(start, { timeout: 3000 }); // timeout 兜底忙/隐藏标签页，最迟 3s 内启动
      return () => cancelIdleCallback(id);
    }
    const t = setTimeout(start, 3000);
    return () => clearTimeout(t);
  }, [nodeList]);

  useEffect(() => {
    const interval = setInterval(() => {
      refresh();
    }, 60000);
    return () => clearInterval(interval);
  }, [refresh]);

  if (isLoading) {
    return <DashboardSkeleton />;
  }
  // 有数据(nodeList 来自快照/上次成功)时错误降级为横幅,不整屏盖掉缓存内容
  if (error && !nodeList) {
    return <div>{t("common.error", { defaultValue: "Error" })}: {error}</div>;
  }
  //#endregion

  return (
    <div className="container mx-auto px-4 space-y-4">
      <Callouts />

      {error && nodeList ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-2 text-sm text-red-400">
          {t("common.error", { defaultValue: "Error" })}: {error}
        </div>
      ) : null}

      <MemoNodeMapView
        nodes={nodeList ?? []}
        liveData={live_data?.data ?? { online: [], data: {} }}
        mapOnly
      />

      <Suspense fallback={<div className="p-4">{t("nodes.loading", { defaultValue: "Loading nodes..." })}</div>}>
        <MemoNodeDisplay
          nodes={nodeList ?? []}
          liveData={live_data?.data ?? { online: [], data: {} }}
        />
      </Suspense>
    </div>
  );
}

