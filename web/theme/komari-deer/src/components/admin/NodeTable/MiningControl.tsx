import { useEffect, useRef, useState } from "react";
import { useRPC2Call } from "@/contexts/RPC2Context";
import { Button } from "@radix-ui/themes";
import { Pickaxe, Pause, Play } from "lucide-react";
import { t } from "i18next";

type MiningControlResp = {
  task_id: string;
  action: string;
  clients: string[];
  queued_clients: string[];
  offline_clients?: string[];
};

type TaskResult = {
  client: string;
  result: string;
  exit_code: number;
};

const POLL_INTERVAL_MS = 2000;
// csnet 的 start 经 supervisord startProcess(startsecs=10) + 看门狗敲门拉矿机，
// 命令总时长可达 ~30s，轮询窗口需覆盖
const POLL_MAX_TIMES = 15;

/**
 * 节点详情抽屉里的挖矿管控（中心端 启动/暂停 子节点挖矿任务）。
 * 走 admin:miningControl 下发，agent 侧按 AGENT_MINER_CONTROL_CMD 模板执行；
 * 结果经任务通道回传，这里轮询 admin:getTaskResultsByTaskId 取本节点结果。
 */
export function MiningControl({ uuid }: { uuid: string }) {
  const { call } = useRPC2Call();
  const [busy, setBusy] = useState<"start" | "stop" | null>(null);
  const [status, setStatus] = useState<"success" | "failed" | null>(null);
  const [message, setMessage] = useState<string>("");
  // 抽屉关闭/组件卸载后终止轮询等悬空副作用，避免对已卸载组件 setState
  const aliveRef = useRef(true);
  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  const pollResult = async (taskId: string) => {
    for (let i = 0; i < POLL_MAX_TIMES; i++) {
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
      if (!aliveRef.current) return;
      try {
        const results = await call<any, TaskResult[]>(
          "admin:getTaskResultsByTaskId",
          { task_id: taskId },
        );
        const mine = (results ?? []).find((r) => r?.client === uuid);
        if (mine) {
          if (!aliveRef.current) return;
          setStatus(mine.exit_code === 0 ? "success" : "failed");
          setMessage((mine.result ?? "").trim());
          return;
        }
      } catch {
        // 轮询失败继续重试，超限后按超时提示
      }
    }
    if (!aliveRef.current) return;
    setStatus("failed");
    setMessage(t("admin.miningControl.pollTimeout", "查询执行结果超时，请稍后在任务结果中查看"));
  };

  const dispatch = async (action: "start" | "stop") => {
    setBusy(action);
    setStatus(null);
    setMessage("");
    try {
      const resp = await call<any, MiningControlResp>("admin:miningControl", {
        clients: [uuid],
        action,
      });
      if (!resp?.task_id) {
        throw new Error("no task id");
      }
      await pollResult(resp.task_id);
    } catch (e) {
      // 原始错误仅入控制台（可能是 i18n 之外的内部文案），UI 统一给可排查的业务提示
      console.warn("[miningControl] dispatch failed:", e);
      if (!aliveRef.current) return;
      setStatus("failed");
      setMessage(
        t("admin.miningControl.dispatchFailed", "下发失败（节点不在线或 agent 不支持）"),
      );
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <label className="text-sm font-medium">
        {t("admin.miningControl.title", "挖矿管控")}
      </label>
      <div className="flex items-center gap-2 flex-wrap">
        <Button
          type="button"
          variant="soft"
          disabled={busy !== null}
          onClick={() => dispatch("start")}
        >
          <Play size={14} />
          {busy === "start"
            ? t("admin.miningControl.starting", "启动中...")
            : t("admin.miningControl.start", "启动挖矿")}
        </Button>
        <Button
          type="button"
          variant="soft"
          color="red"
          disabled={busy !== null}
          onClick={() => dispatch("stop")}
        >
          <Pause size={14} />
          {busy === "stop"
            ? t("admin.miningControl.stopping", "暂停中...")
            : t("admin.miningControl.stop", "暂停挖矿")}
        </Button>
        <Pickaxe size={14} className="text-muted-foreground" />
      </div>
      {status !== null && (
        <span
          className={
            status === "success"
              ? "text-xs text-[#00b875] break-all select-text"
              : "text-xs text-[#e64b73] break-all select-text"
          }
          title={message}
        >
          {status === "success"
            ? t("admin.miningControl.success", "指令已执行") + (message ? `: ${message}` : "")
            : message || t("admin.miningControl.failed", "执行失败")}
        </span>
      )}
      <span className="text-xs text-muted-foreground">
        {t(
          "admin.miningControl.hint",
          "需要节点 agent 配置 AGENT_MINER_CONTROL_CMD 模板（含 {action} 占位符）",
        )}
      </span>
    </div>
  );
}
