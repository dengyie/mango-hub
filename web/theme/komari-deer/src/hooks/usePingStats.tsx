import { useEffect, useState } from 'react';
import { useRPC2Call } from '@/contexts/RPC2Context';

interface PingRecord {
  client: string;
  task_id: number;
  time: string;
  value: number;
}

interface TaskInfo {
  id: number;
  name: string;
  interval: number;
  loss: number;
  value?: number;
  p99?: number;
  p50?: number;
  p99_p50_ratio?: number;
  min?: number;
  max?: number;
  avg?: number;
  latest?: number;
  total?: number;
  type?: string;
}

export interface PingHistoryPoint {
  time: string;
  latency: number | null;
  loss: number | null;
}

export interface PingStats {
  avgLatency: number;
  avgLoss: number;
  avgVolatility: number;
  history: PingHistoryPoint[];
  hasData: boolean;
  /** HTTP 类型任务只按可达性展示，不参与 latency/loss 阈值着色；up=null 表示无采样 */
  httpReachability: { name: string; up: boolean | null }[];
}

const HISTORY_BUCKET_COUNT = 28;

function createEmptyStats(): PingStats {
  return {
    avgLatency: 0,
    avgLoss: 0,
    avgVolatility: 0,
    history: [],
    hasData: false,
    httpReachability: [],
  };
}

function buildPingHistory(
  records: PingRecord[],
  icmpTaskIds: Set<number>,
): PingHistoryPoint[] {
  // 只统计 ICMP 类型任务:HTTP 可达性任务(如「本站-可达性」)的整页延迟
  // 结构上就是几百 ms,混进阈值色条会让卡片满屏假红。
  const icmpRecords = records.filter((record) => icmpTaskIds.has(record.task_id));
  if (icmpRecords.length === 0) {
    return [];
  }

  const sortedRecords = icmpRecords
    .map((record) => ({
      ...record,
      timestamp: new Date(record.time).getTime(),
    }))
    .filter((record) => Number.isFinite(record.timestamp))
    .sort((left, right) => left.timestamp - right.timestamp);

  if (sortedRecords.length === 0) {
    return [];
  }

  const firstTime = sortedRecords[0].timestamp;
  const lastTime = sortedRecords[sortedRecords.length - 1].timestamp;
  const bucketCount = Math.min(HISTORY_BUCKET_COUNT, sortedRecords.length);
  const bucketSize = Math.max(1, (lastTime - firstTime) / bucketCount);

  return Array.from({ length: bucketCount }, (_, index) => {
    const startTime = firstTime + bucketSize * index;
    const endTime = index === bucketCount - 1 ? lastTime + 1 : startTime + bucketSize;
    const bucketRecords = sortedRecords.filter(
      (record) => record.timestamp >= startTime && record.timestamp < endTime,
    );
    const validLatencyRecords = bucketRecords.filter((record) => record.value >= 0);
    const lostRecords = bucketRecords.length - validLatencyRecords.length;
    const latency =
      validLatencyRecords.length > 0
        ? validLatencyRecords.reduce((sum, record) => sum + record.value, 0) / validLatencyRecords.length
        : null;
    const loss = bucketRecords.length > 0 ? (lostRecords / bucketRecords.length) * 100 : null;

    return {
      time: new Date(startTime).toISOString(),
      latency,
      loss,
    };
  });
}

export function usePingStats(uuid: string, hours: number = 24): PingStats {
  const { call } = useRPC2Call();
  const [stats, setStats] = useState<PingStats>(() => createEmptyStats());

  useEffect(() => {
    if (!uuid) return;

    const controller = new AbortController();

    (async () => {
      try {
        type RpcResp = {
          count: number;
          records: PingRecord[];
          tasks?: TaskInfo[];
          from?: string;
          to?: string;
        };

        const result = await call<any, RpcResp>('common:getRecords', {
          uuid,
          type: 'ping',
          hours,
        });

        const records = result?.records || [];
        const tasks = result?.tasks || [];

        if (records.length === 0 || tasks.length === 0) {
          setStats(createEmptyStats());
          return;
        }

        // 按任务类型分流:ICMP 任务进延迟/丢包统计与阈值色条;HTTP 任务只做可达性。
        const icmpTaskIds = new Set<number>();
        const httpTasks: TaskInfo[] = [];
        for (const task of tasks) {
          if (task.type === "http") {
            httpTasks.push(task);
          } else {
            icmpTaskIds.add(task.id);
          }
        }
        const icmpTasks = tasks.filter((task) => icmpTaskIds.has(task.id));

        const history = buildPingHistory(records, icmpTaskIds);
        const latencyValues = icmpTasks
          .map((task) => task.avg ?? task.latest ?? task.value ?? task.p50)
          .filter((value): value is number => typeof value === 'number' && Number.isFinite(value));
        const avgLatency = latencyValues.length > 0
          ? latencyValues.reduce((sum, value) => sum + value, 0) / latencyValues.length
          : 0;

        // Calculate average loss from tasks (ICMP only)
        const totalLoss = icmpTasks.reduce((sum, task) => sum + (task.loss || 0), 0);
        const avgLoss = icmpTasks.length > 0 ? totalLoss / icmpTasks.length : 0;

        // Calculate volatility (p99/p50 ratio average, ICMP only)
        const volatilityValues = icmpTasks
          .filter(task => task.p99_p50_ratio !== undefined && task.p99_p50_ratio > 0)
          .map(task => task.p99_p50_ratio!);

        const avgVolatility = volatilityValues.length > 0
          ? volatilityValues.reduce((sum, val) => sum + val, 0) / volatilityValues.length
          : 0;

        // HTTP 任务可达性:以该任务最新采样为准(value<0 即不可达);窗口内无采样 = unknown
        const httpReachability = httpTasks.map((task) => {
          let up: boolean | null = null;
          let newestTs = -Infinity;
          for (const record of records) {
            if (record.task_id !== task.id) continue;
            const ts = new Date(record.time).getTime();
            if (Number.isFinite(ts) && ts > newestTs) {
              newestTs = ts;
              up = record.value >= 0;
            }
          }
          return { name: task.name, up };
        });

        setStats({
          avgLatency,
          avgLoss,
          avgVolatility,
          history,
          hasData: true,
          httpReachability,
        });
      } catch (err) {
        setStats(createEmptyStats());
      }
    })();

    return () => controller.abort();
  }, [uuid, hours, call]);

  return stats;
}
