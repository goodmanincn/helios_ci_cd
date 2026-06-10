"use client";

// Run 详情页 (T1.6.2)
// 极简版: meta + stages 树, 日志面板留给 T1.6.3, cancel/retry 留给 T1.6.4
// 简化点:
//  - 没用 ui/run-detail.html 的三栏布局 (那个偏后端完整后再做)
//  - 走 AppShell 单列, 顶部 meta + 下方 stages 折叠列表
//  - run.status=running 时 5s 轮询 (M1 没真 stage, 主要看 status 推进)

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";

import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { AppShell } from "@/components/app-shell";
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
      // 跳转到新 run 详情
      router.push(`/runs/${res.id}`);
    } catch (e) {
      setActionErr(e instanceof ApiException ? e.message : String(e));
      setActing(null);
    }
  }, [accessToken, run, router]);

  useEffect(() => {
    // 初始化 fetch — react-hooks/set-state-in-effect 误报, 列表页这是合理模式
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
  }, [load]);

  // running/pending 时轮询 5s
  useEffect(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    if (!run) return;
    if (run.status === "running" || run.status === "pending") {
      pollRef.current = setInterval(load, 5000);
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [run, load]);

  return (
    <AppShell title={run ? `运行 #${run.number}` : "运行详情"}>
      <div className="px-6 py-6 max-w-5xl mx-auto w-full">
        <Breadcrumb run={run} />

        {loading && <div className="card text-sm" style={{ color: "var(--fg-mute)" }}>加载中...</div>}
        {err && <div className="err-msg">{err}</div>}

        {run && !loading && (
          <div className="flex flex-col gap-4">
            <RunMetaCard
              run={run}
              onCancel={onCancel}
              onRetry={onRetry}
              acting={acting}
              actionErr={actionErr}
            />
            <StagesCard stages={run.stages} runStatus={run.status} />
            {accessToken && (
              <LogsPanel runId={run.id} token={accessToken} runStatus={run.status} />
            )}
          </div>
        )}
      </div>
    </AppShell>
  );
}

function Breadcrumb({ run }: { run: RunDetail | null }) {
  return (
    <div className="mb-4 text-sm" style={{ color: "var(--fg-dim)" }}>
      <Link href="/projects" className="hover:underline">项目</Link>
      <span className="mx-2">/</span>
      {run?.project ? (
        <Link href={`/projects/${run.project.id}`} className="hover:underline">{run.project.name}</Link>
      ) : (
        <span>...</span>
      )}
      <span className="mx-2">/</span>
      <span style={{ color: "var(--fg)" }}>{run ? `run #${run.number}` : "..."}</span>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const c = statusBadgeColor(status);
  return (
    <span
      style={{
        color: c.fg,
        background: c.bg,
        padding: "3px 10px",
        borderRadius: 999,
        fontSize: "0.75rem",
        fontWeight: 600,
        textTransform: "lowercase",
        letterSpacing: 0.3,
      }}
    >
      {c.label}
    </span>
  );
}

function RunMetaCard({
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
  const isInFlight = run.status === "pending" || run.status === "running";
  const isTerminal =
    run.status === "success" || run.status === "failed" || run.status === "canceled";
  return (
    <div className="card flex flex-col gap-4">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-3 flex-wrap">
            <h2 className="text-lg font-semibold">运行 #{run.number}</h2>
            <StatusBadge status={run.status} />
          </div>
          {run.message && (
            <div className="text-sm mt-2" style={{ color: "var(--fg-mute)" }}>{run.message}</div>
          )}
        </div>
        <div className="text-xs text-right" style={{ color: "var(--fg-dim)" }}>
          ID {run.id} · pipeline {run.pipeline_id} · v{run.version_id}
        </div>
      </div>

      <div className="flex items-center gap-2 flex-wrap">
        <button
          type="button"
          onClick={onCancel}
          disabled={!isInFlight || acting !== null}
          className="btn-action"
          data-variant="warn"
          title={isInFlight ? "取消运行" : "仅 pending/running 可取消"}
        >
          {acting === "cancel" ? "取消中..." : "取消"}
        </button>
        <button
          type="button"
          onClick={onRetry}
          disabled={!isTerminal || acting !== null}
          className="btn-action"
          data-variant="primary"
          title={isTerminal ? "复制并重新执行" : "仅终态 run 可重试"}
        >
          {acting === "retry" ? "提交中..." : "重试"}
        </button>
        {actionErr && (
          <span
            className="text-xs"
            style={{ color: "#fb7185" }}
          >
            {actionErr}
          </span>
        )}
      </div>

      <div
        className="grid gap-3"
        style={{ gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))" }}
      >
        <Meta label="分支">
          <code className="meta-code">{run.branch || "-"}</code>
        </Meta>
        <Meta label="Commit">
          <code className="meta-code">{shortSHA(run.commit_sha)}</code>
        </Meta>
        <Meta label="触发方式">
          <span>{run.trigger_type || "-"}</span>
        </Meta>
        <Meta label="时长">
          <span>{fmtDuration(run.duration_ms)}</span>
        </Meta>
        <Meta label="开始">
          <span style={{ fontSize: "0.8125rem" }}>{fmtTime(run.started_at)}</span>
        </Meta>
        <Meta label="结束">
          <span style={{ fontSize: "0.8125rem" }}>{fmtTime(run.finished_at)}</span>
        </Meta>
      </div>

      <style jsx>{`
        .meta-code {
          background: var(--bg-elev-2);
          padding: 2px 8px;
          border-radius: 3px;
          font-size: 0.8125rem;
        }
        .btn-action {
          padding: 4px 12px;
          border-radius: 4px;
          font-size: 0.8125rem;
          border: 1px solid var(--border);
          background: var(--bg-elev-2);
          color: var(--fg);
          cursor: pointer;
          transition: background 0.15s, opacity 0.15s;
        }
        .btn-action:hover:not(:disabled) {
          background: var(--bg-elev);
        }
        .btn-action:disabled {
          opacity: 0.4;
          cursor: not-allowed;
        }
        .btn-action[data-variant="primary"]:not(:disabled) {
          border-color: rgba(96, 165, 250, 0.4);
          color: #60a5fa;
        }
        .btn-action[data-variant="warn"]:not(:disabled) {
          border-color: rgba(251, 113, 133, 0.4);
          color: #fb7185;
        }
      `}</style>
    </div>
  );
}

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs" style={{ color: "var(--fg-dim)" }}>{label}</span>
      <span className="text-sm" style={{ color: "var(--fg)" }}>{children}</span>
    </div>
  );
}

function StagesCard({ stages, runStatus }: { stages: Stage[]; runStatus: string }) {
  if (stages.length === 0) {
    return (
      <div
        className="card text-sm"
        style={{ color: "var(--fg-dim)" }}
      >
        <div className="mb-1" style={{ color: "var(--fg-mute)", fontWeight: 600 }}>阶段</div>
        {runStatus === "pending"
          ? "等待执行..."
          : "M1 阶段简化引擎暂未写 stage 细节(整体 run 状态即可代表)。多 stage/step 树将在 M2 接入。"}
      </div>
    );
  }
  return (
    <div className="card flex flex-col gap-3">
      <div className="text-sm" style={{ color: "var(--fg-mute)", fontWeight: 600 }}>阶段</div>
      <div className="flex flex-col gap-2">
        {stages.map((s) => (
          <StageRow key={s.id} s={s} />
        ))}
      </div>
    </div>
  );
}

function StageRow({ s }: { s: Stage }) {
  const [open, setOpen] = useState(false);
  return (
    <div
      style={{
        border: "1px solid var(--border)",
        borderRadius: 6,
        background: "var(--bg-elev-2)",
      }}
    >
      <button
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-3 px-3 py-2 text-left"
        style={{ color: "var(--fg)" }}
      >
        <span style={{ color: "var(--fg-dim)", fontSize: "0.75rem", width: 14 }}>
          {open ? "▾" : "▸"}
        </span>
        <StatusBadge status={s.status || ""} />
        <span className="font-medium">{s.name || s.stage_id}</span>
        <span className="ml-auto text-xs" style={{ color: "var(--fg-dim)" }}>
          {fmtDuration(s.duration_ms)}
        </span>
      </button>
      {open && (
        <div className="px-3 pb-3 pt-1">
          {s.steps.length === 0 ? (
            <div className="text-xs pl-6" style={{ color: "var(--fg-dim)" }}>(无 step 记录)</div>
          ) : (
            <div className="flex flex-col gap-1">
              {s.steps.map((st) => (
                <StepRow key={st.id} st={st} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function StepRow({ st }: { st: Step }) {
  return (
    <div
      className="flex items-center gap-3 px-2 py-1.5 text-sm"
      style={{ borderRadius: 4, background: "var(--bg-elev)" }}
    >
      <StatusBadge status={st.status || ""} />
      <span style={{ color: "var(--fg)" }}>{st.name || `step #${st.id}`}</span>
      {st.exit_code != null && (
        <span className="text-xs" style={{ color: "var(--fg-dim)" }}>
          exit {st.exit_code}
        </span>
      )}
      <span className="ml-auto text-xs" style={{ color: "var(--fg-dim)" }}>
        {fmtDuration(st.duration_ms)}
      </span>
    </div>
  );
}
