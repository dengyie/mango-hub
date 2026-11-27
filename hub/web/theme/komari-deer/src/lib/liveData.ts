import type { LiveDataResponse, Record as LiveRecord } from "@/types/LiveData";
import { parseLivePing } from "./ping";

function finite(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}
function numeric(value: unknown): number { return finite(value) ? value : 0; }

/** Preserve validity separately for legacy charts whose numeric DTO predates unknown states. */
export function normalizeLiveData(value: unknown): LiveDataResponse {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid live response");
  const data: { [uuid: string]: LiveRecord } = {};
  const online: string[] = [];
  for (const [uuid, item] of Object.entries(value)) {
    if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error("Invalid live record");
    const rec = item as Record<string, unknown>;
    if (typeof rec.online !== "boolean") throw new Error("Invalid live online status");
    if (rec.online) online.push(uuid);
    const mining = rec.mining as Record<string, unknown> | undefined;
    const govRaw = mining?.governor as Record<string, unknown> | undefined;
    const govTier = typeof govRaw?.tier === "string" ? govRaw.tier.trim() : "";
    const totalUp = rec.net_total_out ?? rec.net_total_up;
    const totalDown = rec.net_total_in ?? rec.net_total_down;
    const time = typeof rec.time === "string" && /(Z|[+-]\d\d:\d\d)$/.test(rec.time) ? Date.parse(rec.time) : NaN;
    data[uuid] = {
      cpu: { usage: numeric(rec.cpu) }, ram: { used: numeric(rec.ram) }, swap: { used: numeric(rec.swap) },
      disk: { used: numeric(rec.disk) },
      load: { load1: numeric(rec.load), load5: numeric(rec.load5), load15: numeric(rec.load15) },
      network: { up: numeric(rec.net_out), down: numeric(rec.net_in), totalUp: numeric(totalUp), totalDown: numeric(totalDown) },
      validMetrics: {
        cpu: finite(rec.cpu), ram: finite(rec.ram), disk: finite(rec.disk),
        network: [rec.net_out, rec.net_in, totalUp, totalDown].every(finite),
      },
      connections: { tcp: numeric(rec.connections), udp: numeric(rec.connections_udp) },
      gpu: finite(rec.gpu) ? { count: 0, average_usage: rec.gpu, detailed_info: [] } : undefined,
      mining: (mining && finite(mining.hashrate_1min)) || govTier ? {
        // algorithm/hashrate 只在真有矿工数据时才写:只有档位(pause 停矿)时不造假数据,
        // 否则卡片会渲染出一颗「Mining · 0 H/s」的假 pill。
        ...(mining && finite(mining.hashrate_1min) ? {
          algorithm: typeof mining.algorithm === "string" ? mining.algorithm : "",
          hashrate_1min: mining.hashrate_1min,
        } : {}),
        // 只在真有档位时才挂 governor 键:保持老节点 DTO 形状完全不变(历史测试/缓存逐字段比对)
        ...(govTier ? {
          governor: {
            tier: govTier,
            idle_sec: numeric(govRaw?.idle_sec),
            gpu_pct: numeric(govRaw?.gpu_pct),
          },
        } : {}),
      } : undefined,
      ping: rec.ping == null ? {} : parseLivePing(rec.ping),
      uptime: numeric(rec.uptime), process: numeric(rec.process), message: "",
      updated_at: Number.isFinite(time) ? new Date(time).toISOString() : "",
    };
  }
  return { data: { online, data }, status: "ok" };
}
