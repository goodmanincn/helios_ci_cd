"use client";

// Runs 列表页 (T1.6.2)
// 支持 ?project_id= 过滤 (项目详情页跳过来时带上)
// 简化: 单列 table 风格, 游标翻页 (load more), 5s 轮询第一页 (有 running 行时)
//
// query 参数:
//   project_id  数字, 可选, 过滤某个项目
//   status      可选, 单选过滤
//   limit       默认 20

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { AppShell } from "@/components/app-shell";
import { ApiException } from "@/lib/api";
import {
  RunListItem,
  fmtDuration,
  fmtTime,
  listRuns,
  shortSHA,
  statusBadgeColor,
} from "@/lib/runs-api";

const PAGE = 20;

export default function RunsPage() {
  return (
    <AuthGuard require={true}>
      <RunsInner />
    </AuthGuard>
  );
}

function RunsInner() {
  const sp = useSearchParams();
  const projectId = sp?.get("project_id");
  const filterPid = projectId ? Number(projectId) : undefined;
  const [statusFilter, setStatusFilter] = useState<string>("");

  const accessToken = useAuthStore((s) => s.accessToken);
  const [items, setItems] = useState<RunListItem[]>([]);
  const [nextId, setNextId] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const loadFirst = useCallback(async () => {
    if (!accessToken) return;
    setLoading(true);
    setErr(null);
    try {
      const r = await listRuns(accessToken, {
        project_id: filterPid,
        status: statusFilter || undefined,
        limit: PAGE,
      });
      setItems(r.items);
      setNextId(r.next_id);
    } catch (e) {
      setErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [accessToken, filterPid, statusFilter]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadFirst();
  }, [loadFirst]);

  // 有 running/pending 时 5s 轮询第一页 (只刷头部, 不丢游标后内容)
  useEffect(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    const hasInflight = items.some((it) => it.status === "running" || it.status === "pending");
    if (!hasInflight || !accessToken) return;
    pollRef.current = setInterval(async () => {
      try {
        const r = await listRuns(accessToken, {
          project_id: filterPid,
          status: statusFilter || undefined,
          limit: PAGE,
        });
        // 简化: 直接替换第一页, 后续翻页内容也会重置. M1 验收够用.
        setItems(r.items);
        setNextId(r.next_id);
      } catch {
        /* swallow polling errors */
      }
    }, 5000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [items, accessToken, filterPid, statusFilter]);

  async function loadMore() {
    if (!accessToken || nextId == null) return;
    setLoadingMore(true);
    try {
      const r = await listRuns(accessToken, {
        project_id: filterPid,
        status: statusFilter || undefined,
        limit: PAGE,
        before_id: nextId,
      });
      setItems((prev) => [...prev, ...r.items]);
      setNextId(r.next_id);
    } catch (e) {
      setErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <AppShell title="执行记录">
      <div className="px-6 py-6 max-w-5xl mx-auto w-full">
        <div className="flex items-center justify-between mb-4 flex-wrap gap-3">
          <div>
            <h2 className="text-lg font-semibold">执行记录</h2>
            <div className="text-xs mt-1" style={{ color: "var(--fg-dim)" }}>
              {filterPid ? `仅看项目 #${filterPid}` : "全部项目"} · 按 id 倒序
            </div>
          </div>
          <div className="flex items-center gap-2">
            <label className="text-xs" style={{ color: "var(--fg-dim)" }}>状态</label>
            <select
              className="input"
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value)}
              style={{ width: 120 }}
            >
              <option value="">全部</option>
              <option value="pending">排队中</option>
              <option value="running">运行中</option>
              <option value="success">成功</option>
              <option value="failed">失败</option>
              <option value="canceled">已取消</option>
            </select>
          </div>
        </div>

        {err && <div className="err-msg">{err}</div>}
        {loading ? (
          <div className="card text-sm" style={{ color: "var(--fg-mute)" }}>加载中...</div>
        ) : items.length === 0 ? (
          <div
            className="card text-sm"
            style={{ color: "var(--fg-dim)", textAlign: "center", padding: "32px 16px" }}
          >
            暂无执行记录{filterPid ? " (该项目)" : ""}
          </div>
        ) : (
          <div className="card" style={{ padding: 0, overflow: "hidden" }}>
            <table className="w-full" style={{ borderCollapse: "collapse" }}>
              <thead>
                <tr style={{ background: "var(--bg-elev-2)" }}>
                  <Th>#</Th>
                  <Th>状态</Th>
                  <Th>项目</Th>
                  <Th>分支</Th>
                  <Th>Commit</Th>
                  <Th>触发</Th>
                  <Th>时长</Th>
                  <Th>开始时间</Th>
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <RunRow key={it.id} it={it} />
                ))}
              </tbody>
            </table>
          </div>
        )}

        {nextId != null && (
          <div className="mt-4 text-center">
            <button className="btn" onClick={loadMore} disabled={loadingMore}>
              {loadingMore ? "加载中..." : "加载更多"}
            </button>
          </div>
        )}
      </div>
    </AppShell>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th
      style={{
        textAlign: "left",
        padding: "10px 12px",
        fontSize: "0.75rem",
        fontWeight: 600,
        color: "var(--fg-dim)",
        borderBottom: "1px solid var(--border)",
      }}
    >
      {children}
    </th>
  );
}

function Td({ children, mono = false }: { children: React.ReactNode; mono?: boolean }) {
  return (
    <td
      style={{
        padding: "10px 12px",
        fontSize: "0.8125rem",
        color: "var(--fg)",
        borderBottom: "1px solid var(--border)",
        fontFamily: mono ? "var(--font-mono, ui-monospace, monospace)" : undefined,
        whiteSpace: "nowrap",
      }}
    >
      {children}
    </td>
  );
}

function RunRow({ it }: { it: RunListItem }) {
  const c = statusBadgeColor(it.status);
  return (
    <tr style={{ cursor: "pointer" }}>
      <Td>
        <Link
          href={`/runs/${it.id}`}
          style={{ color: "var(--accent)", fontWeight: 600 }}
          className="hover:underline"
        >
          #{it.number}
        </Link>
      </Td>
      <Td>
        <span
          style={{
            color: c.fg,
            background: c.bg,
            padding: "2px 8px",
            borderRadius: 999,
            fontSize: "0.7rem",
            fontWeight: 600,
          }}
        >
          {c.label}
        </span>
      </Td>
      <Td>
        {it.project ? (
          <Link href={`/projects/${it.project.id}`} className="hover:underline" style={{ color: "var(--fg)" }}>
            {it.project.name}
          </Link>
        ) : (
          <span style={{ color: "var(--fg-dim)" }}>-</span>
        )}
      </Td>
      <Td mono>{it.branch || "-"}</Td>
      <Td mono>{shortSHA(it.commit_sha)}</Td>
      <Td>
        <span style={{ color: "var(--fg-mute)" }}>{it.trigger_type || "-"}</span>
      </Td>
      <Td>{fmtDuration(it.duration_ms)}</Td>
      <Td>
        <span style={{ color: "var(--fg-mute)" }}>{fmtTime(it.started_at || it.created_at)}</span>
      </Td>
    </tr>
  );
}
