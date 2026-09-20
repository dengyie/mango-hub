import { useEffect, useRef, useState } from "react";
import { useRPC2Call } from "@/contexts/RPC2Context";
import { useLiveData } from "@/contexts/LiveDataContext";
import { Button } from "@radix-ui/themes";
import { Pickaxe, Pause, Play } from "lucide-react";
import { t } from "i18next";
import {
  interpretMiningControlResp,
  pollMiningTaskResult,
  type MiningControlResp,
} from "@/utils/miningControl";

/**
 * 节点详情抽屉里的挖矿管控（中心端 启动/暂停 子节点挖矿任务）。
 * 走 admin:miningControl 下发，agent 侧按 AGENT_MINER_CONTROL_CMD 模板执行；
 * 结果经任务通道回传，这里轮询 admin:getTaskResultsByTaskId 取本节点结果。
 */
export function MiningControl({ uuid }: { uuid: string }) {
  const { call } = useRPC2Call();
  const { live_data } = useLiveData();
  const onlineList = live_data?.data?.online;
  const isOnline = !onlineList || onlineList.includes(uuid);
  const [busy, setBusy] = useState<"start" | "stop" | null>(null);
  const [status, setStatus] = useState<"success" | "failed" | "queued" | null>(null);
  const [message, setMessage] = useState<string>("");
  // 抽屉关闭/组件卸载后终止轮询等悬空副作用，避免对已卸载组件 setState
  const aliveRef = useRef(true);
  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  const fail = (text: string) => {
    if (!aliveRef.current) return;
    setBusy(null);
    setStatus("failed");
    setMessage(text);
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
      const outcome = interpretMiningControlResp(resp, uuid);
      if (outcome.kind === "failed") {
        fail(
          t("admin.nodeDetail.miningControl.dispatchFailed", "下发失败（节点不在线或 agent 不支持）"),
        );
        return;
      }
      if (outcome.kind === "queued") {
        if (!aliveRef.current) return;
        setBusy(null);
        setStatus("queued");
        setMessage(
          t("admin.nodeDetail.miningControl.queued", "指令已排队，节点重连后执行"),
        );
        return;
      }
      const poll = await pollMiningTaskResult(call, uuid, outcome.taskId, {
        isAlive: () => aliveRef.current,
      });
      if (!aliveRef.current) return;
      if (poll.kind === "result") {
        setBusy(null);
        setStatus(poll.exitCode === 0 ? "success" : "failed");
        setMessage(poll.message);
        return;
      }
      fail(
        t("admin.nodeDetail.miningControl.pollTimeout", "节点未在 30s 内回传执行结果"),
      );
    } catch (e) {
      // 原始错误仅入控制台（可能是 i18n 之外的内部文案），UI 统一给可排查的业务提示
      console.warn("[miningControl] dispatch failed:", e);
      fail(
        t("admin.nodeDetail.miningControl.dispatchFailed", "下发失败（节点不在线或 agent 不支持）"),
      );
    }
  };

  const controlsDisabled = busy !== null || !isOnline;

  return (
    <div className="flex flex-col gap-3">
      <label className="text-sm font-medium">
        {t("admin.nodeDetail.miningControl.title", "挖矿管控")}
      </label>
      <div className="flex items-center gap-2 flex-wrap">
        <Button
          type="button"
          variant="soft"
          disabled={controlsDisabled}
          onClick={() => dispatch("start")}
        >
          <Play size={14} />
          {busy === "start"
            ? t("admin.nodeDetail.miningControl.starting", "启动中...")
            : t("admin.nodeDetail.miningControl.start", "启动挖矿")}
        </Button>
        <Button
          type="button"
          variant="soft"
          color="red"
          disabled={controlsDisabled}
          onClick={() => dispatch("stop")}
        >
          <Pause size={14} />
          {busy === "stop"
            ? t("admin.nodeDetail.miningControl.stopping", "暂停中...")
            : t("admin.nodeDetail.miningControl.stop", "暂停挖矿")}
        </Button>
        <Pickaxe size={14} className="text-muted-foreground" />
      </div>
      {status !== null && (
        <span
          className={
            status === "failed"
              ? "text-xs text-[#e64b73] break-all select-text"
              : status === "queued"
                ? "text-xs text-[#d97706] break-all select-text"
                : "text-xs text-[#00b875] break-all select-text"
          }
          title={message}
        >
          {status === "success"
            ? t("admin.nodeDetail.miningControl.success", "指令已执行") +
              (message ? `: ${message}` : "")
            : message ||
              (status === "queued"
                ? t("admin.nodeDetail.miningControl.queued", "指令已排队，节点重连后执行")
                : t("admin.nodeDetail.miningControl.failed", "执行失败"))}
        </span>
      )}
      <span className="text-xs text-muted-foreground">
        {t(
          "admin.nodeDetail.miningControl.hint",
          "需要节点 agent 配置 AGENT_MINER_CONTROL_CMD 模板（含 {action} 占位符）",
        )}
      </span>
    </div>
  );
}
