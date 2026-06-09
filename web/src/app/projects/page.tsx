"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { AppShell } from "@/components/app-shell";
import {
  Project,
  deleteProject,
  listProjects,
} from "@/lib/projects-api";
import { ApiException } from "@/lib/api";

export default function ProjectsPage() {
  return (
    <AuthGuard require={true}>
      <ProjectsInner />
    </AuthGuard>
  );
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

  const load = useCallback(async () => {
    if (!accessToken) return;
    setLoading(true);
    setErr(null);
    try {
      const res = await listProjects(accessToken, { q, limit: 50 });
      setItems(res.items);
      setTotal(res.total);
    } catch (e) {
      setErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [accessToken, q]);

  useEffect(() => {
    const t = setTimeout(load, 250); // debounce 搜索输入
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

  return (
    <AppShell
      title="项目"
      actions={
        <Link href="/projects/new" className="btn btn-primary text-sm">
          + 新建项目
        </Link>
      }
    >
      <div className="px-6 py-6 max-w-6xl mx-auto w-full">
        <div className="mb-4 flex items-center gap-3">
          <input
            className="input flex-1 max-w-md"
            placeholder="搜索项目 (名称 / slug)"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          <div className="text-xs" style={{ color: "var(--fg-dim)" }}>
            共 {total} 个项目
          </div>
        </div>

        {err && (
          <div className="err-msg mb-4">{err}</div>
        )}

        {loading ? (
          <EmptyOrLoading text="加载中..." />
        ) : items.length === 0 ? (
          <EmptyState onCreate={() => router.push("/projects/new")} />
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {items.map((p) => (
              <ProjectCard
                key={p.id}
                project={p}
                onDelete={() => onDelete(p)}
                deleting={deleting === p.id}
              />
            ))}
          </div>
        )}
      </div>
    </AppShell>
  );
}

function ProjectCard({
  project,
  onDelete,
  deleting,
}: {
  project: Project;
  onDelete: () => void;
  deleting: boolean;
}) {
  const visBadge = project.visibility === "public" ? "公开" : "私有";
  return (
    <div
      className="rounded-md p-4 flex flex-col gap-2 transition"
      style={{
        background: "var(--bg-elev)",
        border: "1px solid var(--border)",
      }}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <Link
            href={`/projects/${project.id}`}
            className="font-semibold truncate hover:underline"
            style={{ color: "var(--fg)" }}
          >
            {project.name}
          </Link>
          <div className="text-xs mt-0.5" style={{ color: "var(--fg-dim)" }}>
            {project.slug}
          </div>
        </div>
        <span
          className="text-[10px] px-2 py-0.5 rounded uppercase tracking-wider"
          style={{
            background: "var(--bg-elev-2)",
            color: project.visibility === "public" ? "var(--accent)" : "var(--fg-mute)",
          }}
        >
          {visBadge}
        </span>
      </div>

      {project.description && (
        <p
          className="text-xs line-clamp-2"
          style={{ color: "var(--fg-mute)" }}
        >
          {project.description}
        </p>
      )}

      <div className="flex items-center gap-2 text-xs mt-1" style={{ color: "var(--fg-dim)" }}>
        <span className="uppercase">{project.repo_type}</span>
        <span>·</span>
        <span className="truncate" title={project.repo_url}>{shortRepo(project.repo_url)}</span>
      </div>

      <div className="flex items-center gap-2 mt-2 text-xs">
        <span style={{ color: "var(--fg-dim)" }}>分支:</span>
        <code style={{ background: "var(--bg-elev-2)", padding: "1px 6px", borderRadius: 3 }}>
          {project.default_branch}
        </code>
        <div className="flex-1" />
        <button
          className="text-xs"
          style={{ color: "var(--danger)" }}
          onClick={onDelete}
          disabled={deleting}
        >
          {deleting ? "删除中..." : "删除"}
        </button>
      </div>
    </div>
  );
}

function EmptyOrLoading({ text }: { text: string }) {
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
      <div className="text-3xl mb-3" style={{ color: "var(--fg-dim)" }}>◇</div>
      <p className="text-sm mb-1" style={{ color: "var(--fg-mute)" }}>还没有项目</p>
      <p className="text-xs mb-4" style={{ color: "var(--fg-dim)" }}>新建项目即可关联 Git 仓库开始构建</p>
      <button className="btn btn-primary" onClick={onCreate}>+ 新建第一个项目</button>
    </div>
  );
}

function shortRepo(url: string): string {
  // https://github.com/acme/api.git -> acme/api
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
