import { useEffect, useLayoutEffect } from "react";

// 首帧敏感逻辑(快照读取 / 路由落定)须在首帧绘制前同步落定,客户端走 useLayoutEffect;
// SSG 阶段走 useEffect 避免服务端 useLayoutEffect 警告。isomorphic 两态统一出口。
export const useIsomorphicLayoutEffect: typeof useEffect =
  typeof window === "undefined" ? useEffect : useLayoutEffect;