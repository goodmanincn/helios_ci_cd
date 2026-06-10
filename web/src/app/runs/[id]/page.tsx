"use client";

// Run 详情页 (T1.6.2 + T1.7.4: 对齐 ui/run-detail.html 三栏布局)
//
// 布局:
//   ┌─ 顶部 meta 条: 状态 pill / #number / pipeline 名 / cancel+retry 按钮
//   ├─ 元信息行: 分支 / commit / 触发方式 / 触发人 / 时长
//   ├─ 水平 stage rail (流程进度条, MVP 简化版)
//   ├─ 下方两栏 (280px 步骤树 | 1fr 日志):
//   │    aside: 阶段/步骤 树, 步骤点击 → 选中
//   │    main:  LogsPanel (M1 是 run-level, 显示选中 step 名做上下文)
//
// M1 简化: 后端常 0 stages (简化引擎), 走 fallback 虚拟单 step "build" 给 UI 呈现。
// running/pending 时 5s 轮询 run.status 推进 (主要看终态过渡)。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";

import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { AppShell } from "@/components/app-shell";
import { ApprovalPanel } from "@/components/approval-panel";
import { LogsPanel } from "@/components/logs-panel";
import { ApiException } from "@/lib/api";
import {
  RunDetail,
  Stage,
  Step,
  cancelRun,
  fmtDuration,
  fmtTime,
  getRun,
  retryRun,
  shortSHA,
  statusBadgeColor,
} from "@/lib/runs-api";

export default function RunDetailPage() {
  return (
    <AuthGuard require={true}>
      <RunDetailInner />
    </AuthGuard>
  );
}

interface SelectedStep {
  stageId: number;
  stepId: number;
  label: string;
}

function RunDetailInner() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const accessToken = useAuthStore((s) => s.accessToken);
  const id = Number(params?.id);

  const [run, setRun] = useState<RunDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [actionErr, setActionErr] = useState<string | null>(null);
  const [acting, setActing] = useState<"cancel" | "retry" | null>(null);
  const [selected, setSelected] = useState<SelectedStep | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const load = useCallback(async () => {
    if (!accessToken || !Number.isFinite(id)) return;
    try {
      const r = await getRun(accessToken, id);
      setRun(r);
      setErr(null);
    } catch (e) {
      setErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [accessToken, id]);

  const onCancel = useCallback(async () => {
    if (!accessToken || !run) return;
    if (!window.confirm(`确认取消运行 #${run.number}?`)) return;
    setActing("cancel");
    setActionErr(null);
    try {
      await cancelRun(accessToken, run.id);
      await load();
    } catch (e) {
      setActionErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setActing(null);
    }
  }, [accessToken, run, load]);

  const onRetry = useCallback(async () => {
    if (!accessToken || !run) return;
    if (!window.confirm(`复制运行 #${run.number} 并重新执行?`)) return;
    setActing("retry");
    setActionErr(null);
    try {
      const res = await retryRun(accessToken, run.id);
      router.push(`/runs/${res.id}`);
    } catch (e) {
      setActionErr(e instanceof ApiException ? e.message : String(e));
      setActing(null);
    }
  }, [accessToken, run, router]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
  }, [load]);

  // running/pending/approval 时轮询 5s
  useEffect(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    if (!run) return;
    if (
      run.status === "running" ||
      run.status === "pending" ||
      run.status === "approval"
    ) {
      pollRef.current = setInterval(load, 5000);
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [run, load]);

  // 选中第一个步骤 (有 stage 时), 或虚拟 build
  useEffect(() => {
    if (!run || selected) return;
    const first = firstSelectableStep(run.stages);
    if (first) setSelected(first); // eslint-disable-line react-hooks/set-state-in-effect
  }, [run, selected]);

  // breadcrumb title: 项目名 #run.number
  const title = run
    ? `${run.project?.name ?? "运行"} #${run.number}`
    : "运行详情";

  return (
    <AppShell title={title}>
      <div className="rd-root">
        {loading && (
          <div className="rd-loading">加载中...</div>
        )}
        {err && <div className="err-msg" style={{ margin: 24 }}>{err}</div>}

        {run && !loading && (
          <>
            <RunMetaStrip
              run={run}
              onCancel={onCancel}
              onRetry={onRetry}
              acting={acting}
              actionErr={actionErr}
            />

            {run.status === "approval" && run.approval_requests && run.approval_requests.length > 0 && (
              <ApprovalPanel
                runId={run.id}
                requests={run.approval_requests}
                onChanged={load}
              />
            )}

            <StageRail stages={run.stages} runStatus={run.status} />

            <div className="rd-body">
              <StepTreeSidebar
                stages={run.stages}
                runStatus={run.status}
                selected={selected}
                onSelect={setSelected}
              />
              <div className="rd-log-wrap">
                <div className="rd-log-head">
                  <span className="rd-log-crumb">
                    {selected?.label || "等待中..."}
                  </span>
                  {accessToken && run.status === "running" && (
                    <span className="rd-log-live">● 实时</span>
                  )}
                </div>
                {accessToken && (
                  <div className="rd-log-pane">
                    <LogsPanel
                      runId={run.id}
                      token={accessToken}
                      runStatus={run.status}
                    />
                  </div>
                )}
              </div>
            </div>
          </>
        )}
      </div>

      <style jsx>{`
        .rd-root {
          display: flex;
          flex-direction: column;
          /* AppShell header 高 52px, 让本页占满剩余视口 (而非依赖父级 flex 撑) */
          min-height: calc(100vh - 52px);
        }
        .rd-loading {
          padding: 24px;
          font-size: 14px;
          color: var(--fg-mute);
        }
        .rd-body {
          flex: 1;
          display: grid;
          grid-template-columns: 280px 1fr;
          min-height: 0;
        }
        @media (max-width: 900px) {
          .rd-body {
            grid-template-columns: 220px 1fr;
          }
        }
        @media (max-width: 700px) {
          .rd-body {
            grid-template-columns: 1fr;
          }
        }
        .rd-log-wrap {
          display: flex;
          flex-direction: column;
          min-width: 0;
          min-height: 0;
        }
        .rd-log-head {
          display: flex;
          align-items: center;
          gap: 8px;
          padding: 10px 16px;
          border-bottom: 1px solid var(--border);
          background: rgba(255, 255, 255, 0.01);
          font-family: ui-monospace, Menlo, monospace;
          font-size: 12px;
          color: var(--fg-mute);
        }
        .rd-log-crumb {
          flex: 1;
        }
        .rd-log-live {
          font-size: 11px;
          color: #60a5fa;
          font-weight: 600;
        }
        .rd-log-pane {
          flex: 1;
          padding: 12px;
          min-height: 0;
          overflow: auto;
        }
      `}</style>
    </AppShell>
  );
}

// ========== 顶部 meta 条 ==========

function RunMetaStrip({
  run,
  onCancel,
  onRetry,
  acting,
  actionErr,
}: {
  run: RunDetail;
  onCancel: () => void;
  onRetry: () => void;
  acting: "cancel" | "retry" | null;
  actionErr: string | null;
}) {
  const isInFlight =
    run.status === "pending" || run.status === "running" || run.status === "approval";
  const isTerminal =
    run.status === "success" ||
    run.status === "failed" ||
    run.status === "canceled" ||
    run.status === "timeout";
  const c = statusBadgeColor(run.status);

  return (
    <div className="rd-meta">
      <div className="rd-meta-top">
        <span className="rd-pill" style={{ color: c.fg, background: c.bg }}>
          <span className="rd-pill-dot" style={{ background: c.fg }} />
          {c.label}
        </span>
        <span className="rd-num">#{run.number}</span>
        <h1 className="rd-title">
          {run.project?.name ?? `pipeline ${run.pipeline_id}`}
        </h1>
        <div className="rd-actions">
          <button
            type="button"
            onClick={onRetry}
            disabled={!isTerminal || acting !== null}
            className="rd-btn"
            data-variant="primary"
            title={isTerminal ? "复制并重新执行" : "仅终态 run 可重试"}
          >
            ↻ {acting === "retry" ? "提交中..." : "重新运行"}
          </button>
          <button
            type="button"
            onClick={onCancel}
            disabled={!isInFlight || acting !== null}
            className="rd-btn"
            data-variant="warn"
            title={isInFlight ? "取消运行" : "仅 pending/running 可取消"}
          >
            ⏹ {acting === "cancel" ? "取消中..." : "取消"}
          </button>
        </div>
      </div>

      <div className="rd-meta-row">
        <span>
          🌿 <code>{run.branch || "-"}</code>
        </span>
        <span>
          📦 <code>{shortSHA(run.commit_sha) || "-"}</code>
        </span>
        <span>🎯 由 {run.trigger_type || "-"} 触发</span>
        <span>⏱ {fmtRunTime(run)}</span>
        <span className="rd-meta-id">
          ID {run.id} · pipeline {run.pipeline_id} · v{run.version_id}
        </span>
      </div>

      {run.message && (
        <div className="rd-meta-msg">{run.message}</div>
      )}
      {actionErr && (
        <div className="rd-meta-err">{actionErr}</div>
      )}

      <style jsx>{`
        .rd-meta {
          padding: 18px 24px 14px;
          border-bottom: 1px solid var(--border);
          display: flex;
          flex-direction: column;
          gap: 8px;
        }
        .rd-meta-top {
          display: flex;
          align-items: center;
          gap: 10px;
          flex-wrap: wrap;
        }
        .rd-pill {
          display: inline-flex;
          align-items: center;
          gap: 6px;
          padding: 3px 10px;
          font-size: 11px;
          font-weight: 600;
          border-radius: 9999px;
          text-transform: lowercase;
        }
        .rd-pill-dot {
          width: 6px;
          height: 6px;
          border-radius: 50%;
        }
        .rd-num {
          font-family: ui-monospace, Menlo, monospace;
          color: var(--fg-dim);
          font-size: 12px;
        }
        .rd-title {
          font-size: 18px;
          font-weight: 590;
          color: var(--fg);
          margin: 0;
        }
        .rd-actions {
          margin-left: auto;
          display: flex;
          gap: 8px;
        }
        .rd-btn {
          padding: 4px 12px;
          font-size: 12px;
          font-weight: 510;
          border-radius: 5px;
          border: 1px solid var(--border);
          background: rgba(255, 255, 255, 0.02);
          color: var(--fg-mute);
          cursor: pointer;
          transition: background 0.15s, opacity 0.15s;
        }
        .rd-btn:hover:not(:disabled) {
          background: rgba(255, 255, 255, 0.05);
          color: var(--fg);
        }
        .rd-btn:disabled {
          opacity: 0.4;
          cursor: not-allowed;
        }
        .rd-btn[data-variant="primary"]:not(:disabled) {
          border-color: rgba(96, 165, 250, 0.4);
          color: #60a5fa;
        }
        .rd-btn[data-variant="warn"]:not(:disabled) {
          border-color: rgba(251, 113, 133, 0.4);
          color: #fb7185;
        }
        .rd-meta-row {
          display: flex;
          align-items: center;
          gap: 16px;
          flex-wrap: wrap;
          color: var(--fg-mute);
          font-size: 12px;
        }
        .rd-meta-row code {
          font-family: ui-monospace, Menlo, monospace;
          background: var(--bg-elev-2);
          padding: 1px 6px;
          border-radius: 3px;
          font-size: 11px;
        }
        .rd-meta-id {
          margin-left: auto;
          color: var(--fg-dim);
          font-size: 11px;
        }
        .rd-meta-msg {
          font-size: 13px;
          color: var(--fg-mute);
          margin-top: 2px;
        }
        .rd-meta-err {
          font-size: 12px;
          color: #fb7185;
        }
      `}</style>
    </div>
  );
}

// ========== 水平 stage rail ==========

function StageRail({
  stages,
  runStatus,
}: {
  stages: Stage[];
  runStatus: string;
}) {
  // M1 简化引擎常 0 stages, 用 run 状态生成虚拟单节点 build
  const items = useMemo(() => {
    if (stages.length > 0) {
      return stages.map((s) => ({
        key: String(s.id),
        label: s.name || s.stage_id,
        status: s.status || "pending",
        duration: fmtDuration(s.duration_ms),
      }));
    }
    return [
      {
        key: "virtual-build",
        label: "build",
        status: runStatus,
        duration: "",
      },
    ];
  }, [stages, runStatus]);

  return (
    <div className="rd-rail-wrap">
      <div className="rd-rail">
        {items.map((it, i) => (
          <RailNode key={it.key} item={it} last={i === items.length - 1} />
        ))}
      </div>

      <style jsx>{`
        .rd-rail-wrap {
          padding: 14px 24px;
          border-bottom: 1px solid var(--border);
          overflow-x: auto;
        }
        .rd-rail {
          display: flex;
          align-items: center;
          gap: 8px;
          min-width: max-content;
        }
      `}</style>
    </div>
  );
}

function RailNode({
  item,
  last,
}: {
  item: { key: string; label: string; status: string; duration: string };
  last: boolean;
}) {
  const tone = railTone(item.status);
  return (
    <>
      <div className="rd-stg">
        <span className="rd-stgd" data-tone={tone} />
        <div className="rd-stgl">
          {item.label}
          {(item.duration || isTransient(item.status)) && (
            <span className="rd-stgt">
              {item.duration}
              {isTransient(item.status) && (item.duration ? " · " : "") + statusText(item.status)}
            </span>
          )}
        </div>
      </div>
      {!last && <div className="rd-stgn" data-tone={tone === "ok" ? "ok" : "dm"} />}

      <style jsx>{`
        .rd-stg {
          display: flex;
          flex-direction: column;
          align-items: center;
          gap: 6px;
          min-width: 120px;
        }
        .rd-stgd {
          width: 14px;
          height: 14px;
          border-radius: 50%;
          background: var(--fg-dim);
          border: 2px solid var(--bg);
        }
        .rd-stgd[data-tone="ok"] {
          background: #27a644;
        }
        .rd-stgd[data-tone="rn"] {
          background: #3b82f6;
          box-shadow: 0 0 0 4px rgba(59, 130, 246, 0.2);
          animation: pl 1.5s infinite;
        }
        .rd-stgd[data-tone="dn"] {
          background: #ef4444;
        }
        .rd-stgd[data-tone="wn"] {
          background: #f59e0b;
        }
        .rd-stgl {
          font-size: 11px;
          color: var(--fg-mute);
          text-align: center;
          white-space: nowrap;
        }
        .rd-stgt {
          display: block;
          font-size: 10px;
          color: var(--fg-dim);
          margin-top: 2px;
        }
        .rd-stgn {
          flex: 1;
          height: 2px;
          background: var(--fg-dim);
          min-width: 30px;
        }
        .rd-stgn[data-tone="ok"] {
          background: #27a644;
        }
        .rd-stgn[data-tone="dm"] {
          background: rgba(255, 255, 255, 0.08);
        }
        @keyframes pl {
          0%,
          100% {
            opacity: 1;
          }
          50% {
            opacity: 0.4;
          }
        }
      `}</style>
    </>
  );
}

// ========== 左侧 stage/step 树 ==========

function StepTreeSidebar({
  stages,
  runStatus,
  selected,
  onSelect,
}: {
  stages: Stage[];
  runStatus: string;
  selected: SelectedStep | null;
  onSelect: (s: SelectedStep) => void;
}) {
  // 0 stage 时生成虚拟节点
  const virtualMode = stages.length === 0;

  return (
    <aside className="rd-side">
      <div className="rd-side-title">阶段 / 步骤</div>

      {virtualMode ? (
        <div
          className={`rd-stx${selected?.stepId === 0 ? " sel" : ""}`}
          data-tone={runStatus}
          onClick={() =>
            onSelect({ stageId: 0, stepId: 0, label: "build" })
          }
        >
          <span className="rd-stx-mark">{markFor(runStatus)}</span>
          <span className="rd-stx-name">build</span>
          <span className="rd-stx-tm">{statusText(runStatus)}</span>
        </div>
      ) : (
        stages.map((s) => (
          <div key={s.id} className="rd-stage-group">
            <div className="rd-stx" data-tone={s.status}>
              <span className="rd-stx-mark">{markFor(s.status || "")}</span>
              <span className="rd-stx-name">{s.name || s.stage_id}</span>
              <span className="rd-stx-tm">{fmtDuration(s.duration_ms)}</span>
            </div>
            {s.steps.map((st) => {
              const isSel = selected?.stepId === st.id;
              return (
                <div
                  key={st.id}
                  className={`rd-stx child${isSel ? " sel" : ""}`}
                  data-tone={st.status}
                  onClick={() =>
                    onSelect({
                      stageId: s.id,
                      stepId: st.id,
                      label: `${s.name || s.stage_id} › ${st.name || `step #${st.id}`}`,
                    })
                  }
                >
                  <span className="rd-stx-mark">{markFor(st.status || "")}</span>
                  <span className="rd-stx-name">{st.name || `step #${st.id}`}</span>
                  <span className="rd-stx-tm">{fmtDuration(st.duration_ms)}</span>
                </div>
              );
            })}
          </div>
        ))
      )}

      <style jsx>{`
        .rd-side {
          border-right: 1px solid var(--border);
          overflow-y: auto;
          padding: 14px 10px;
          background: var(--bg-elev);
        }
        .rd-side-title {
          font-size: 11px;
          font-weight: 510;
          color: var(--fg-dim);
          text-transform: uppercase;
          letter-spacing: 0.4px;
          padding: 0 8px 10px;
        }
        .rd-stage-group {
          margin-bottom: 6px;
        }
        :global(.rd-stx) {
          display: flex;
          align-items: center;
          gap: 6px;
          padding: 6px 10px;
          font-size: 12px;
          color: var(--fg-mute);
          border-radius: 5px;
          cursor: pointer;
          transition: background 0.12s;
        }
        :global(.rd-stx:hover) {
          background: rgba(255, 255, 255, 0.03);
          color: var(--fg);
        }
        :global(.rd-stx.child) {
          padding-left: 22px;
          font-size: 11px;
        }
        :global(.rd-stx.sel) {
          background: var(--accent-soft);
          color: var(--fg);
        }
        :global(.rd-stx[data-tone="success"]) {
          color: #86efac;
        }
        :global(.rd-stx[data-tone="running"]) {
          color: #93c5fd;
        }
        :global(.rd-stx[data-tone="failed"]) {
          color: #fca5a5;
        }
        :global(.rd-stx[data-tone="pending"]) {
          color: var(--fg-dim);
        }
        :global(.rd-stx-mark) {
          width: 14px;
          display: inline-block;
          text-align: center;
        }
        :global(.rd-stx-name) {
          flex: 1;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }
        :global(.rd-stx-tm) {
          margin-left: auto;
          font-size: 10px;
          color: var(--fg-dim);
          font-family: ui-monospace, Menlo, monospace;
        }
      `}</style>
    </aside>
  );
}

// ========== 工具 ==========

function railTone(status: string): "ok" | "rn" | "dn" | "wn" | "pd" {
  switch (status) {
    case "success":
      return "ok";
    case "running":
      return "rn";
    case "failed":
    case "canceled":
      return "dn";
    case "approval":
      return "wn";
    case "timeout":
      return "dn";
    case "pending":
      return "pd";
    default:
      return "pd";
  }
}

function markFor(status: string): string {
  switch (status) {
    case "success":
      return "✓";
    case "running":
      return "●";
    case "failed":
      return "✗";
    case "canceled":
      return "⊘";
    case "approval":
      return "👤";
    case "timeout":
      return "⏱";
    case "pending":
      return "○";
    default:
      return "○";
  }
}

function statusText(status: string): string {
  switch (status) {
    case "success":
      return "完成";
    case "running":
      return "运行中";
    case "failed":
      return "失败";
    case "canceled":
      return "已取消";
    case "approval":
      return "待审批";
    case "timeout":
      return "超时";
    case "pending":
      return "等待";
    default:
      return status;
  }
}

function isTransient(status: string): boolean {
  return status === "running" || status === "pending";
}

function firstSelectableStep(stages: Stage[]): SelectedStep | null {
  for (const s of stages) {
    if (s.steps.length > 0) {
      const st = s.steps[0];
      return {
        stageId: s.id,
        stepId: st.id,
        label: `${s.name || s.stage_id} › ${st.name || `step #${st.id}`}`,
      };
    }
  }
  if (stages.length > 0) {
    return null;
  }
  // virtual fallback: build
  return { stageId: 0, stepId: 0, label: "build" };
}

function fmtRunTime(run: RunDetail): string {
  if (run.duration_ms) return `时长 ${fmtDuration(run.duration_ms)}`;
  if (run.started_at) return `开始于 ${fmtTime(run.started_at)}`;
  if (run.created_at) return `创建于 ${fmtTime(run.created_at)}`;
  return "-";
}

// 兼容旧 import (避免破坏其他文件) — 仅声明类型
export type _RunDetailKeepStep = Step;
