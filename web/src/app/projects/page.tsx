"use client";

// Projects 页 — T1.7.3 对齐 ui/projects.html 原型
//
// 关键点:
//  - 顶部 3 段: 页头 + 指标卡 (本周构建/成功率/平均时长/部署目标) + 筛选行
//  - 卡片 3 列响应式网格, 项目卡 = 彩色图标 + 名称 + 描述 + 最近 run 徽章 + 底部摘要行
//  - 网格最后一格固定 "+ 新建项目" 虚线占位卡
//  - run summary 由前端做次级聚合: 拉 listRuns(limit=200) 后按 pipeline_id 收最新一条 + 计数
//    后端没有 latest_run 字段, 这是 M1 优雅降级 (M2 加 /api/v1/projects 的 with_run_summary)
//  - 数据缺失统一显示 "--", 不强渲染增量徽章

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { AppShell } from "@/components/app-shell";
import { Project, deleteProject, listProjects } from "@/lib/projects-api";
import { RunListItem, listRuns } from "@/lib/runs-api";
import { ApiException } from "@/lib/api";

export default function ProjectsPage() {
  return (
    <AuthGuard require={true}>
      <ProjectsInner />
    </AuthGuard>
  );
}

interface ProjectRunSummary {
  latestRun?: RunListItem;
  runCount: number;
}

function ProjectsInner() {
  const router = useRouter();
  const accessToken = useAuthStore((s) => s.accessToken);
  const [items, setItems] = useState<Project[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [deleting, setDeleting] = useState<number | null>(null);
  // 聚合的 run 摘要: key = project.id (注意: runs 用 pipeline_id, 但 RunListItem.project.id 即项目)
  const [runSummary, setRunSummary] = useState<Map<number, ProjectRunSummary>>(
    new Map(),
  );

  const load = useCallback(async () => {
    if (!accessToken) return;
    setLoading(true);
    setErr(null);
    try {
      const res = await listProjects(accessToken, { q, limit: 50 });
      setItems(res.items);
      setTotal(res.total);

      // 拉最近 200 条 run 做次级聚合 (优雅降级: 失败留空 Map)
      try {
        const r = await listRuns(accessToken, { limit: 200 });
        const m = new Map<number, ProjectRunSummary>();
        for (const run of r.items) {
          const pid = run.project?.id;
          if (pid == null) continue;
          const cur = m.get(pid);
          if (!cur) {
            m.set(pid, { latestRun: run, runCount: 1 });
          } else {
            cur.runCount += 1;
            // listRuns 默认按 id desc, 第一次写入的就是最新
          }
        }
        setRunSummary(m);
      } catch {
        setRunSummary(new Map());
      }
    } catch (e) {
      setErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [accessToken, q]);

  useEffect(() => {
    const t = setTimeout(load, 250);
    return () => clearTimeout(t);
  }, [load]);

  async function onDelete(p: Project) {
    if (!accessToken) return;
    if (!confirm(`删除项目 "${p.name}" (slug=${p.slug}) 吗?此操作不可撤销。`)) return;
    setDeleting(p.id);
    try {
      await deleteProject(accessToken, p.id);
      await load();
    } catch (e) {
      alert(e instanceof ApiException ? e.message : String(e));
    } finally {
      setDeleting(null);
    }
  }

  // ===== 衍生指标 (后端没有专门聚合接口, 用 runSummary 推) =====
  const metrics = useMemo(() => {
    const allRuns: RunListItem[] = [];
    runSummary.forEach((v) => {
      if (v.latestRun) allRuns.push(v.latestRun);
    });
    // 本周构建数 — 用 7 天内创建的 run (从 runSummary 不够, 这里仅看最近 run 是否本周)
    // M1 简化: 无法精准聚合本周构建总数 (需要后端 metrics), 这里降级到 "--"
    const totalRuns = Array.from(runSummary.values()).reduce(
      (s, v) => s + v.runCount,
      0,
    );
    // 成功率: 用 latestRun 中 status=success 的占比 (粗略)
    const term = allRuns.filter(
      (r) => r.status === "success" || r.status === "failed" || r.status === "canceled",
    );
    const succ = term.filter((r) => r.status === "success").length;
    const successRate = term.length > 0 ? (succ / term.length) * 100 : null;
    // 平均时长 (ms) — 用所有有 duration 的 run
    const dur = allRuns
      .map((r) => r.duration_ms)
      .filter((d): d is number => typeof d === "number" && d > 0);
    const avgMs =
      dur.length > 0 ? dur.reduce((a, b) => a + b, 0) / dur.length : null;
    return {
      totalRuns: totalRuns > 0 ? totalRuns : null,
      successRate,
      avgMs,
    };
  }, [runSummary]);

  return (
    <AppShell
      title="项目"
      actions={
        <Link href="/projects/new" className="hdr-cta">
          + 新建项目
        </Link>
      }
    >
      <div className="p-content">
        {/* 页头 + 副标题 */}
        <div className="ch">
          <div>
            <h1>项目</h1>
            <div className="sub">
              {total} 个活跃项目 · 本周构建 {metrics.totalRuns ?? "--"} 次 · 成功率{" "}
              {metrics.successRate != null
                ? `${metrics.successRate.toFixed(1)}%`
                : "--"}
            </div>
          </div>
          <div className="ch-actions">
            <button className="btn btn-ghost" type="button" disabled title="M2 接入">
              导入 Git 仓库
            </button>
            <Link href="/projects/new" className="btn btn-primary">
              + 新建项目
            </Link>
          </div>
        </div>

        {/* 4 张指标卡 */}
        <div className="metrics-grid">
          <MetricCard
            label="本周构建"
            value={metrics.totalRuns != null ? String(metrics.totalRuns) : "--"}
            hint={metrics.totalRuns != null ? "近 200 条统计" : "数据不足"}
          />
          <MetricCard
            label="成功率"
            value={
              metrics.successRate != null
                ? `${metrics.successRate.toFixed(1)}%`
                : "--"
            }
            hint={metrics.successRate != null ? "终态 run 抽样" : "数据不足"}
          />
          <MetricCard
            label="平均时长"
            value={metrics.avgMs != null ? fmtDurShort(metrics.avgMs) : "--"}
            hint={metrics.avgMs != null ? "最近 run 抽样" : "数据不足"}
          />
          <MetricCard
            label="部署目标"
            value="--"
            hint="M3 接入"
          />
        </div>

        {/* 筛选行 */}
        <div className="filter-row">
          <div className="filter-search">
            <span className="filter-search-icon">🔍</span>
            <input
              className="filter-search-input"
              placeholder="搜索项目..."
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          <button className="btn btn-sm" type="button" disabled>
            所有项目 ⌄
          </button>
          <button className="btn btn-sm" type="button" disabled>
            所有状态 ⌄
          </button>
          <button className="btn btn-sm" type="button" disabled>
            最近活动 ⌄
          </button>
          <div className="filter-count">
            显示 {items.length} / {total}
          </div>
        </div>

        {err && <div className="err-msg mb-4">{err}</div>}

        {loading ? (
          <PlaceholderCard text="加载中..." />
        ) : items.length === 0 ? (
          <EmptyState onCreate={() => router.push("/projects/new")} />
        ) : (
          <div className="proj-grid">
            {items.map((p) => (
              <ProjectCard
                key={p.id}
                project={p}
                summary={runSummary.get(p.id)}
                onDelete={() => onDelete(p)}
                deleting={deleting === p.id}
              />
            ))}
            {/* 末尾占位卡 */}
            <Link href="/projects/new" className="proj-new">
              <div className="proj-new-plus">+</div>
              <div className="proj-new-title">新建项目</div>
              <div className="proj-new-hint">从 Git 仓库导入或从模板创建</div>
            </Link>
          </div>
        )}
      </div>

      <style jsx>{`
        .p-content {
          padding: 28px 32px;
          max-width: 1400px;
          margin: 0 auto;
        }
        .hdr-cta {
          display: inline-flex;
          align-items: center;
          padding: 5px 12px;
          background: var(--accent);
          color: white;
          border-radius: 5px;
          font-size: 12px;
          font-weight: 510;
          text-decoration: none;
        }
        .hdr-cta:hover {
          background: var(--accent-2);
        }
        .ch {
          display: flex;
          align-items: flex-end;
          justify-content: space-between;
          margin-bottom: 24px;
          flex-wrap: wrap;
          gap: 16px;
        }
        .ch h1 {
          font-size: 24px;
          font-weight: 590;
          letter-spacing: -0.288px;
          line-height: 1.33;
        }
        .ch .sub {
          margin-top: 4px;
          color: var(--fg-mute);
          font-size: 13px;
        }
        .ch-actions {
          display: flex;
          align-items: center;
          gap: 8px;
        }
        .metrics-grid {
          display: grid;
          grid-template-columns: repeat(4, 1fr);
          gap: 14px;
          margin-bottom: 28px;
        }
        @media (max-width: 900px) {
          .metrics-grid {
            grid-template-columns: repeat(2, 1fr);
          }
        }
        .filter-row {
          display: flex;
          align-items: center;
          gap: 8px;
          margin-bottom: 14px;
          flex-wrap: wrap;
        }
        .filter-search {
          display: flex;
          align-items: center;
          gap: 8px;
          padding: 4px 10px;
          background: rgba(255, 255, 255, 0.02);
          border: 1px solid var(--border);
          border-radius: 5px;
          min-width: 280px;
        }
        .filter-search-icon {
          color: var(--fg-dim);
          font-size: 12px;
        }
        .filter-search-input {
          flex: 1;
          background: transparent;
          border: none;
          outline: none;
          color: var(--fg);
          font-size: 12px;
        }
        .filter-search-input::placeholder {
          color: var(--fg-dim);
        }
        .filter-count {
          margin-left: auto;
          color: var(--fg-dim);
          font-size: 12px;
        }
        :global(.btn-sm) {
          padding: 3px 8px;
          font-size: 12px;
        }
        .proj-grid {
          display: grid;
          grid-template-columns: repeat(3, 1fr);
          gap: 14px;
        }
        @media (max-width: 1100px) {
          .proj-grid {
            grid-template-columns: repeat(2, 1fr);
          }
        }
        @media (max-width: 720px) {
          .proj-grid {
            grid-template-columns: 1fr;
          }
        }
        .proj-new {
          border: 1px dashed var(--border-strong);
          border-radius: 8px;
          background: transparent;
          display: flex;
          flex-direction: column;
          align-items: center;
          justify-content: center;
          min-height: 160px;
          color: var(--fg-dim);
          text-decoration: none;
          transition: border-color 0.15s, color 0.15s;
        }
        .proj-new:hover {
          border-color: var(--accent);
          color: var(--accent);
        }
        .proj-new-plus {
          font-size: 24px;
        }
        .proj-new-title {
          font-size: 13px;
          font-weight: 510;
          margin-top: 4px;
          color: var(--fg-mute);
        }
        .proj-new-hint {
          font-size: 11px;
          margin-top: 4px;
          color: var(--fg-dim);
        }
      `}</style>
    </AppShell>
  );
}

// ========== 子组件 ==========

function MetricCard({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint: string;
}) {
  return (
    <div className="metric-card">
      <div className="metric-label">{label}</div>
      <div className="metric-value">{value}</div>
      <span className="metric-pill">
        <span className="metric-dot" />
        {hint}
      </span>

      <style jsx>{`
        .metric-card {
          background: rgba(255, 255, 255, 0.02);
          border: 1px solid var(--border);
          border-radius: 8px;
          padding: 16px;
        }
        .metric-label {
          font-size: 12px;
          color: var(--fg-mute);
        }
        .metric-value {
          font-size: 24px;
          font-weight: 590;
          margin: 4px 0;
          color: var(--fg);
        }
        .metric-pill {
          display: inline-flex;
          align-items: center;
          gap: 5px;
          padding: 2px 9px;
          font-size: 11px;
          font-weight: 510;
          border: 1px solid var(--border);
          border-radius: 9999px;
          color: var(--fg-dim);
        }
        .metric-dot {
          width: 6px;
          height: 6px;
          border-radius: 50%;
          background: var(--fg-dim);
        }
      `}</style>
    </div>
  );
}

function ProjectCard({
  project,
  summary,
  onDelete,
  deleting,
}: {
  project: Project;
  summary?: ProjectRunSummary;
  onDelete: () => void;
  deleting: boolean;
}) {
  const icon = iconForRepo(project);
  const status = summary?.latestRun?.status;
  const badge = projectBadge(status);
  return (
    <div className="proj-cd">
      <div className="proj-head">
        <div
          className="proj-ic"
          style={{ background: icon.bg }}
          title={project.repo_type}
        >
          {icon.emoji}
        </div>
        <div className="proj-main">
          <Link href={`/projects/${project.id}`} className="proj-name">
            {project.name}
          </Link>
          <div className="proj-repo">
            {shortRepo(project.repo_url)} · {project.default_branch}
          </div>
        </div>
        <span className={`pill pill-${badge.tone}`}>
          <span className="pill-dot" />
          {badge.label}
        </span>
      </div>

      <div className="proj-desc">
        {project.description || (
          <span style={{ color: "var(--fg-dim)" }}>
            {project.repo_type.toUpperCase()} · {project.visibility === "public" ? "公开" : "私有"}
          </span>
        )}
      </div>

      <div className="proj-foot">
        <span title={summary?.latestRun?.finished_at || summary?.latestRun?.started_at || ""}>
          {footTimeIcon(status)} {footTimeText(summary)}
        </span>
        <code className="proj-sha">{shortSHA(summary?.latestRun?.commit_sha) || "--"}</code>
        <span className="proj-runs">{summary?.runCount ?? 0} runs</span>
        <button
          type="button"
          className="proj-delete"
          onClick={(e) => {
            e.preventDefault();
            onDelete();
          }}
          disabled={deleting}
        >
          {deleting ? "..." : "删除"}
        </button>
      </div>

      <style jsx>{`
        .proj-cd {
          background: rgba(255, 255, 255, 0.02);
          border: 1px solid var(--border);
          border-radius: 8px;
          padding: 16px;
          display: flex;
          flex-direction: column;
          gap: 12px;
          transition: border-color 0.15s;
        }
        .proj-cd:hover {
          border-color: var(--border-strong);
        }
        .proj-head {
          display: flex;
          align-items: center;
          gap: 10px;
        }
        .proj-ic {
          width: 32px;
          height: 32px;
          border-radius: 6px;
          display: flex;
          align-items: center;
          justify-content: center;
          font-size: 16px;
          flex-shrink: 0;
        }
        .proj-main {
          flex: 1;
          min-width: 0;
        }
        .proj-name {
          font-size: 13px;
          font-weight: 590;
          color: var(--fg);
          display: block;
          text-decoration: none;
        }
        .proj-name:hover {
          color: var(--accent);
        }
        .proj-repo {
          font-family: ui-monospace, Menlo, monospace;
          font-size: 11px;
          color: var(--fg-dim);
          margin-top: 2px;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }
        .proj-desc {
          font-size: 12px;
          color: var(--fg-mute);
          min-height: 18px;
          display: -webkit-box;
          -webkit-line-clamp: 2;
          -webkit-box-orient: vertical;
          overflow: hidden;
        }
        .proj-foot {
          display: flex;
          align-items: center;
          gap: 8px;
          font-size: 11px;
          color: var(--fg-dim);
          border-top: 1px solid var(--border);
          padding-top: 10px;
        }
        .proj-sha {
          font-family: ui-monospace, Menlo, monospace;
          font-size: 11px;
        }
        .proj-runs {
          margin-left: auto;
        }
        .proj-delete {
          background: transparent;
          border: none;
          color: var(--danger);
          font-size: 11px;
          cursor: pointer;
          padding: 0 4px;
        }
        .proj-delete:disabled {
          opacity: 0.5;
          cursor: not-allowed;
        }
        :global(.pill) {
          display: inline-flex;
          align-items: center;
          gap: 5px;
          padding: 2px 9px;
          font-size: 11px;
          font-weight: 510;
          border: 1px solid var(--border);
          border-radius: 9999px;
          color: var(--fg-mute);
          white-space: nowrap;
        }
        :global(.pill-dot) {
          width: 6px;
          height: 6px;
          border-radius: 50%;
          background: var(--fg-dim);
        }
        :global(.pill-ok) {
          color: #4ade80;
          border-color: rgba(74, 222, 128, 0.3);
          background: rgba(39, 166, 68, 0.08);
        }
        :global(.pill-ok .pill-dot) {
          background: #27a644;
        }
        :global(.pill-dn) {
          color: #f87171;
          border-color: rgba(248, 113, 113, 0.3);
          background: rgba(239, 68, 68, 0.08);
        }
        :global(.pill-dn .pill-dot) {
          background: #ef4444;
        }
        :global(.pill-wn) {
          color: #fbbf24;
          border-color: rgba(251, 191, 36, 0.3);
          background: rgba(245, 158, 11, 0.08);
        }
        :global(.pill-wn .pill-dot) {
          background: #f59e0b;
        }
        :global(.pill-rn) {
          color: #60a5fa;
          border-color: rgba(96, 165, 250, 0.3);
          background: rgba(59, 130, 246, 0.08);
        }
        :global(.pill-rn .pill-dot) {
          background: #3b82f6;
          animation: pl 1.5s infinite;
        }
        :global(.pill-nu) {
          color: var(--fg-dim);
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
    </div>
  );
}

function PlaceholderCard({ text }: { text: string }) {
  return (
    <div
      className="rounded-md flex items-center justify-center py-12 text-sm"
      style={{
        background: "var(--bg-elev-2)",
        border: "1px dashed var(--border-strong)",
        color: "var(--fg-mute)",
      }}
    >
      {text}
    </div>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div
      className="rounded-md flex flex-col items-center justify-center py-16"
      style={{
        background: "var(--bg-elev-2)",
        border: "1px dashed var(--border-strong)",
      }}
    >
      <div className="text-3xl mb-3" style={{ color: "var(--fg-dim)" }}>
        ◇
      </div>
      <p className="text-sm mb-1" style={{ color: "var(--fg-mute)" }}>
        还没有项目
      </p>
      <p className="text-xs mb-4" style={{ color: "var(--fg-dim)" }}>
        新建项目即可关联 Git 仓库开始构建
      </p>
      <button className="btn btn-primary" onClick={onCreate}>
        + 新建第一个项目
      </button>
    </div>
  );
}

// ========== 工具函数 ==========

function shortRepo(url: string): string {
  try {
    if (url.startsWith("git@")) {
      const [, rest] = url.split(":");
      return rest?.replace(/\.git$/, "") ?? url;
    }
    const u = new URL(url);
    return u.pathname.replace(/^\//, "").replace(/\.git$/, "");
  } catch {
    return url;
  }
}

function shortSHA(s?: string): string {
  return s ? s.slice(0, 7) : "";
}

function fmtDurShort(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rs = Math.floor(s % 60);
  return `${m}m ${rs}s`;
}

// 按 repo_type 给项目卡片配色 / 图标 (缺省紫色立方体)
function iconForRepo(p: Project): { emoji: string; bg: string } {
  switch (p.repo_type) {
    case "github":
      return { emoji: "📦", bg: "linear-gradient(135deg,#5e6ad2,#7170ff)" };
    case "gitlab":
      return { emoji: "🦊", bg: "linear-gradient(135deg,#f59e0b,#ef4444)" };
    case "gitee":
      return { emoji: "🚀", bg: "linear-gradient(135deg,#dc2626,#f59e0b)" };
    case "bitbucket":
      return { emoji: "🪣", bg: "linear-gradient(135deg,#2563eb,#22d3ee)" };
    default:
      return { emoji: "📦", bg: "linear-gradient(135deg,#5e6ad2,#7170ff)" };
  }
}

// projectBadge: 基于最近 run 的 status 映射到卡片右上角 pill
function projectBadge(status?: string): { tone: string; label: string } {
  switch (status) {
    case "success":
      return { tone: "ok", label: "passing" };
    case "running":
    case "pending":
      return { tone: "rn", label: "building" };
    case "failed":
      return { tone: "dn", label: "failed" };
    case "canceled":
      return { tone: "wn", label: "canceled" };
    default:
      return { tone: "nu", label: "idle" };
  }
}

function footTimeIcon(status?: string): string {
  if (status === "success") return "✓";
  if (status === "failed") return "❌";
  if (status === "canceled") return "✖";
  if (status === "running" || status === "pending") return "🏗";
  return "·";
}

function footTimeText(s?: ProjectRunSummary): string {
  if (!s?.latestRun) return "尚未运行";
  const ts = s.latestRun.finished_at || s.latestRun.started_at || s.latestRun.created_at;
  return ago(ts) ?? "—";
}

function ago(iso?: string): string | null {
  if (!iso) return null;
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return null;
  const diff = Date.now() - t;
  if (diff < 0) return "刚刚";
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s} 秒前`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分钟前`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} 小时前`;
  const d = Math.floor(h / 24);
  return `${d} 天前`;
}
