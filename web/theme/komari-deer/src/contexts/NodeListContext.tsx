"use client";

import React from "react";
import { useRPC2Call } from "./RPC2Context";
import { useIsomorphicLayoutEffect } from "@/hooks/useIsomorphicLayoutEffect";
import { stringToBytes } from "@/utils/unitHelper";
import { loadSnapshot, saveNodeListSnapshot } from "@/lib/snapshotCache";

export type NodeBasicInfo = {
  /** 节点唯一标识符 */
  uuid: string;
  /** 节点名称 */
  name: string;
  /** CPU型号 */
  cpu_name: string;
  /** 虚拟化 */
  virtualization: string;
  /** 系统架构 */
  arch: string;
  /** CPU核心数 */
  cpu_cores: number;
  /** 操作系统 */
  os: string;
  /** 内核版本 */
  kernel_version: string;
  /** GPU型号 */
  gpu_name: string;
  /** 地区标识 */
  region: string;
  /** 总内存(字节) */
  mem_total: number;
  /** 总交换空间(字节) */
  swap_total: number;
  /** 总磁盘空间(字节) */
  disk_total: number;
  /** 版本号 */
  version: string;
  /** 权重 */
  weight: number;
  /** 价格 */
  price: number;
  tags: string;
  /** 账单周期（天）*/
  billing_cycle: number;
  /** 货币 */
  currency: string;
  /** 分组 */
  group: string;
  /** 流量阈值 */
  traffic_limit: number;
  /** 流量阈值类型 */
  traffic_limit_type: undefined | "sum" | "max" | "min" | "up" | "down";
  /** 过期时间 */
  expired_at: string;
  /** 创建时间 */
  created_at: string;
  /** 更新时间 */
  updated_at: string;
  ipv4?: string; 
  ipv6?: string;
};

interface NodeListContextType {
  nodeList: NodeBasicInfo[] | null;
  isLoading: boolean;
  error: string | null;
  refresh: () => void;
}

const NodeListContext = React.createContext<NodeListContextType | undefined>(
  undefined
);

function normalizeTrafficLimit(value: unknown): number {
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) return 0;

    // Strings with units or expressions should always be parsed as byte sizes.
    if (/[a-zA-Z\s,*]/.test(trimmed)) {
      return stringToBytes(trimmed);
    }

    const numeric = Number(trimmed);
    if (!Number.isFinite(numeric) || numeric <= 0) return 0;

    // Backward compatibility: some payloads appear to send plain GB counts.
    if (numeric < 1024 ** 2) {
      return numeric * 1024 ** 3;
    }

    return numeric;
  }

  if (typeof value === "number") {
    if (!Number.isFinite(value) || value <= 0) return 0;

    // Backward compatibility: treat tiny raw numbers as legacy GiB limits.
    if (value < 1024 ** 2) {
      return value * 1024 ** 3;
    }

    return value;
  }

  return 0;
}

export const NodeListProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [nodeList, setNodeList] = React.useState<NodeBasicInfo[] | null>(null);
  const [isLoading, setIsLoading] = React.useState<boolean>(true);
  const [error, setError] = React.useState<string | null>(null);
  const { call } = useRPC2Call();

  const refresh = () => {
    // 不能 setIsLoading(true):60s 定时刷新会把骨架盖回快照/已有数据上,
    // 首帧水合 setIsLoading(false) 与「失败保数据」的降级都会被它破坏。
    // 也不能在开头 setError(null):断网时每次 60s 重试会把错误横幅清掉又弹回来(闪烁),
    // 错误只在真正拿到结果(.then)时才清。
    // 通过 RPC2 获取节点基本信息
    call<{ uuid?: string }, Record<string, any>>("common:getNodes")
      .then((result) => {
        setError(null);
        if (!result || typeof result !== "object") {
          setNodeList([]);
          return;
        }
        // 将 { [uuid]: Client } 转换为 NodeBasicInfo[]
        const list: NodeBasicInfo[] = Object.values(result).map((n: any) => ({
          uuid: n.uuid,
          name: n.name,
          cpu_name: n.cpu_name,
          virtualization: n.virtualization,
          arch: n.arch,
          cpu_cores: n.cpu_cores,
          os: n.os,
          kernel_version: n.kernel_version,
          gpu_name: n.gpu_name,
          region: n.region,
          mem_total: n.mem_total,
          swap_total: n.swap_total,
          disk_total: n.disk_total,
          // 兼容旧字段，若无版本信息则给空串
          version: n.version ?? "",
          weight: n.weight ?? 0,
          price: n.price ?? 0,
          tags: n.tags ?? "",
          billing_cycle: n.billing_cycle ?? 0,
          currency: n.currency ?? "",
          group: n.group ?? "",
          traffic_limit: normalizeTrafficLimit(n.traffic_limit),
          traffic_limit_type: n.traffic_limit_type,
          expired_at: n.expired_at ?? "",
          created_at: n.created_at ?? "",
          updated_at: n.updated_at ?? "",
          ipv4: n.ipv4,
          ipv6: n.ipv6,
        }));
        setNodeList(list);
        saveNodeListSnapshot(list);
      })
      .catch((err: any) => {
        setError(err?.message || "An error occurred while fetching data");
        // 失败时保留快照/上次数据(降级为横幅而非整屏错误,对齐 LiveData 轮询的既有行为)
      })
      .finally(() => {
        setIsLoading(false);
      });
  };

  // 首帧水合:先读上次快照立即渲染(跳过骨架等网络),网络数据到达后由 refresh 覆盖。
  // useLayoutEffect 让快照在首帧绘制前落定,详情页硬加载不先闪一帧骨架。
  useIsomorphicLayoutEffect(() => {
    const snap = loadSnapshot();
    if (snap?.nodeList && Array.isArray(snap.nodeList) && snap.nodeList.length > 0) {
      setNodeList(snap.nodeList);
      setIsLoading(false);
    }
  }, []);

  React.useEffect(() => {
    refresh();
  }, []);
  return (
    <NodeListContext.Provider value={{ nodeList, isLoading, error, refresh }}>
      {children}
    </NodeListContext.Provider>
  );
};

export const useNodeList = () => {
  const context = React.useContext(NodeListContext);
  if (!context) {
    throw new Error("useNodeList must be used within a NodeListProvider");
  }
  return context;
};
