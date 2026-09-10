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
 * 识别依据优先用锚点 uuid（强识别），保留兼容性节点名含 `Azure`（当前固定宿主节点
 * 为 `[香港]Azure-VPS`，uuid `7ff47295-1d4c-4443-b72a-e6f81a56b527`）。即使宿主机将来
 * 改名（名称含 azure）只要 uuid 不变仍能识别；也不会因新增某台名字含 azure 的非宿主机而误显。
 */

const CNB_HOST_NAME_PATTERN = /azure/i;
/** anchor uuid —— 即使宿主机改名也按此强识别（当前承载 CNB 反代的节点）。 */
const CNB_HOST_UUID = "7ff47295-1d4c-4443-b72a-e6f81a56b527";

/** 该可达性任务是否为「CNB 反代探活」任务（仅 CNB 宿主需要展示）。 */
function isCnbReachabilityTaskName(name: string): boolean {
  const n = (name ?? "").toLowerCase();
  return n.includes("反代") && n.includes("cnb");
}

/** 判断给定节点是否就是承载 CNB 反代/额度数据的宿主节点。 */
export function isCnbProxyHost(
  node: Pick<NodeBasicInfo, "name" | "uuid"> | undefined | null,
): boolean {
  if (!node) return false;
  if (node.uuid && node.uuid.toLowerCase() === CNB_HOST_UUID) return true;
  return CNB_HOST_NAME_PATTERN.test(node.name ?? "");
}

/**
 * 过滤掉「CNB-AI-反代」可达性标签：对非 CNB 宿主节点隐藏该 label，
 * 因为它是后端 default_on 任务自动加给所有新增探针的，只有 CNB 反代宿主才需要。
 */
export function filterCnbReachability(
  items: { name: string; up: boolean | null; availability: number | null }[],
  isHost: boolean,
): { name: string; up: boolean | null; availability: number | null }[] {
  if (isHost) return items;
  return items.filter((item) => !isCnbReachabilityTaskName(item?.name ?? ""));
}