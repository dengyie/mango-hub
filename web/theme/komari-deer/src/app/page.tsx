"use client";

import { useState } from "react";
import InstancePage from "@/components/instance/InstancePage";
import DashboardContent from "@/components/DashboardContent";
import StaticShell from "@/components/StaticShell";
import { useIsomorphicLayoutEffect } from "@/hooks/useIsomorphicLayoutEffect";

/**
 * 静态导出 SPA 的路由决策。详情页走 hash 路由(/#/instance/<uuid>),
 * 使硬加载时 URL path 恒为 "/",首帧永远与 index.html 一致,根除 React #418。
 */
type Route = { name: "instance"; uuid: string } | { name: "dashboard" } | "pending";

function parseHash(hash: string): Exclude<Route, "pending"> {
  const m = hash.match(/^#\/instance\/([^/?#]+)/);
  if (m?.[1]) {
    // hash 里是 URL 编码后的 uuid;解码让 header 显示原始串、与 nodeList.find 匹配。
    // 坏编码保留原样;hex-dash UUID 恒为 no-op。
    let uuid = m[1];
    try {
      uuid = decodeURIComponent(m[1]);
    } catch {
      // 保留编码串
    }
    return { name: "instance", uuid };
  }
  return { name: "dashboard" };
}

export default function Page() {
  // pending = 路由未判定。SSR/SSG 与客户端一致渲染 StaticShell(中性壳,非空 main),
  // 水合后 layout effect 在首帧前写入真实路由,两态渲染恒一致,零 #418 也无白屏/骨架闪烁。
  const [route, setRoute] = useState<Route>("pending");

  useIsomorphicLayoutEffect(() => {
    // 旧式 /instance/<uuid> 深链 → 重定向到 hash 路由(uuid 做 URL 编码)
    const parts = window.location.pathname.split("/").filter(Boolean);
    if (parts[0] === "instance" && parts[1]) {
      window.history.replaceState(
        null,
        "",
        `/#/instance/${encodeURIComponent(parts[1])}`
      );
    }
    setRoute(parseHash(window.location.hash));

    const onHashChange = () => setRoute(parseHash(window.location.hash));
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  if (route === "pending") return <StaticShell />;
  if (route.name === "instance") return <InstancePage uuid={route.uuid} />;
  return <DashboardContent />;
}
