import type { NodeBasicInfo } from "@/contexts/NodeListContext";

/**
 * CNB AI 反代/额度 —— 单节点归属判定。
 *
 * 背景：CNB 额度卡（/ops/quota）与「CNB-AI-反代」HTTP 可达性标签
 * 都属于「承载 CNB 反代（cnb-ai.mangoqwq.com）」的这一台主机（[香港]Azure-VPS）。
 * 后端把这个反代可达性任务标记为 default_on（新探针自动开启），导致新增探针
 * 也会带上「CNB-AI-反代」；同时前端 LoadChart 把 QuotaChart 无条件渲染在
 * 每个节点弹窗里（embedded 模式），导致所有 VPS 都显示「CNB AI Quota」。
 *
 * 因此前端统一用「是否该节点就是 CNB 反代宿主」来闸门这两处内容：
 *   - 只有该节点显示 CNB AI Quota 额度卡；
 *   - 只有该节点的 Ping Stats 显示「CNB-AI-反代」可达性标签。
 * 这样即使后端仍把该可达性任务分配给新探针，新增探针也不会再出现 CNB 相关标签。
 *
 * 识别依据：节点名称含 `Azure`（当前固定宿主节点为 `[香港]Azure-VPS`）。
 * 若以后反代宿主机改名/换机，只需改这里/迁移即可整体切换。
 */

const CNB_HOST_NAME_PATTERN = /azure/i;

/** 该可达性任务是否为「CNB 反代探活」任务（仅 CNB 宿主需要展示）。 */
function isCnbReachabilityTaskName(name: string): boolean {
  const n = (name ?? "").toLowerCase();
  return n.includes("反代") && n.includes("cnb");
}

/** 判断给定节点是否就是承载 CNB 反代/额度数据的宿主节点。 */
export function isCnbProxyHost(node: Pick<NodeBasicInfo, "name"> | undefined | null): boolean {
  if (!node) return false;
  return CNB_HOST_NAME_PATTERN.test(node.name ?? "");
}

/**
 * 过滤掉「CNB-AI-反代」可达性标签：对非 CNB 宿主节点隐藏该 label，
 * 因为它是后端 default_on 任务自动加给所有新增探针的，只有 CNB 反代宿主才需要。
 */
export function filterCnbReachability(
  items: { name: string; up: boolean | null }[],
  isHost: boolean,
): { name: string; up: boolean | null }[] {
  if (isHost) return items;
  return items.filter((item) => !isCnbReachabilityTaskName(item?.name ?? ""));
}