import type { LivePing } from "@/lib/ping";

export type LiveData = {
    online: string[];
    data: { [key: string]: Record };
};

export type Record = {
  ping?: LivePing;
  validMetrics?: { cpu: boolean; ram: boolean; disk: boolean; network: boolean };
  cpu: {
    usage: number;
  };
  ram: {
    used: number;
  };
  swap: {
    used: number;
  };
  load: {
    load1: number;
    load5: number;
    load15: number;
  };
  disk: {
    used: number;
  };
  network: {
    up: number;
    down: number;
    totalUp: number;
    totalDown: number;
  };
  connections: {
    tcp: number;
    udp: number;
  };
  gpu?: {
    count: number;
    average_usage: number;
    detailed_info: {
      name: string;
      memory_total: number;
      memory_used: number;
      utilization: number;
      temperature: number;
    }[];
  };
  mining?: {
    /** 只有档位、没有矿工(pause 停矿)时缺省,不要按空串当算法名渲染。 */
    algorithm?: string;
    hashrate_1min?: number;
    /** power_governor 自适应功率档位;未部署/调节器停写时缺省。 */
    governor?: {
      tier: string;
      idle_sec?: number;
      gpu_pct?: number;
    };
  };
  uptime: number;
  process: number;
  message: string;
  updated_at: string;
};

export type LiveDataResponse = {
  data: LiveData;
  status: string;
};
