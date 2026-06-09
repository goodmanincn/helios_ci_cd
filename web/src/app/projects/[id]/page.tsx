"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { AppShell } from "@/components/app-shell";
import {
  Project,
  deleteProject,
  getProject,
  updateProject,
} from "@/lib/projects-api";
import { ApiException } from "@/lib/api";

export default function ProjectDetailPage() {
  return (
    <AuthGuard require={true}>
      <ProjectDetailInner />
    </AuthGuard>
  );
}

function ProjectDetailInner() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const accessToken = useAuthStore((s) => s.accessToken);
  const id = Number(params?.id);

  const [project, setProject] = useState<Project | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [draftDesc, setDraftDesc] = useState("");
  const [draftBranch, setDraftBranch] = useState("");
  const [draftVis, setDraftVis] = useState<"private" | "public">("private");

  const load = useCallback(async () => {
    if (!accessToken || !Number.isFinite(id)) return;
    setLoading(true);
    setErr(null);
    try {
      const p = await getProject(accessToken, id);
      setProject(p);
      setDraftName(p.name);
      setDraftDesc(p.description ?? "");
      setDraftBranch(p.default_branch);
      setDraftVis(p.visibility);
    } catch (e) {
      setErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [accessToken, id]);

  useEffect(() => { load(); }, [load]);

  async function onSave() {
    if (!accessToken || !project) return;
    setSaving(true);
    try {
      const upd = await updateProject(accessToken, project.id, {
        name: draftName !== project.name ? draftName : undefined,
        description: draftDesc !== (project.description ?? "") ? draftDesc : undefined,
        default_branch: draftBranch !== project.default_branch ? draftBranch : undefined,
        visibility: draftVis !== project.visibility ? draftVis : undefined,
      });
      setProject(upd);
      setEditing(false);
    } catch (e) {
      alert(e instanceof ApiException ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  async function onDelete() {
    if (!accessToken || !project) return;
    if (!confirm(`删除项目 "${project.name}"?此操作不可撤销。`)) return;
    try {
      await deleteProject(accessToken, project.id);
      router.replace("/projects");
    } catch (e) {
      alert(e instanceof ApiException ? e.message : String(e));
    }
  }

  return (
    <AppShell title={project ? project.name : "项目详情"}>
      <div className="px-6 py-6 max-w-3xl mx-auto w-full">
        <div className="mb-4 text-sm" style={{ color: "var(--fg-dim)" }}>
          <Link href="/projects" className="hover:underline">项目</Link>
          <span className="mx-2">/</span>
          <span style={{ color: "var(--fg)" }}>{project ? project.name : "..."}</span>
        </div>

        {loading && <div className="card text-sm" style={{ color: "var(--fg-mute)" }}>加载中...</div>}
        {err && <div className="err-msg">{err}</div>}

        {project && !loading && (
          <div className="card flex flex-col gap-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0 flex-1">
                {editing ? (
                  <input
                    className="input text-lg font-semibold"
                    value={draftName}
                    onChange={(e) => setDraftName(e.target.value)}
                  />
                ) : (
                  <h2 className="text-lg font-semibold">{project.name}</h2>
                )}
                <div className="text-xs mt-1 font-mono" style={{ color: "var(--fg-dim)" }}>
                  slug: {project.slug} · org: {project.org_id} · 创建于 {fmt(project.created_at)}
                </div>
              </div>
              {!editing ? (
                <div className="flex gap-2">
                  <button className="btn" onClick={() => setEditing(true)}>编辑</button>
                  <button className="btn" style={{ color: "var(--danger)" }} onClick={onDelete}>
                    删除
                  </button>
                </div>
              ) : (
                <div className="flex gap-2">
                  <button className="btn" onClick={() => { setEditing(false); load(); }}>取消</button>
                  <button className="btn btn-primary" onClick={onSave} disabled={saving}>
                    {saving ? "保存中..." : "保存"}
                  </button>
                </div>
              )}
            </div>

            <Row label="描述">
              {editing ? (
                <textarea
                  className="input min-h-16"
                  value={draftDesc}
                  onChange={(e) => setDraftDesc(e.target.value)}
                />
              ) : (
                <span style={{ color: project.description ? "var(--fg)" : "var(--fg-dim)" }}>
                  {project.description || "(未填写)"}
                </span>
              )}
            </Row>

            <Row label="仓库">
              <span className="font-mono text-xs">
                <span className="uppercase mr-2" style={{ color: "var(--accent)" }}>{project.repo_type}</span>
                {project.repo_url}
              </span>
            </Row>

            <Row label="默认分支">
              {editing ? (
                <input
                  className="input font-mono text-sm w-48"
                  value={draftBranch}
                  onChange={(e) => setDraftBranch(e.target.value)}
                />
              ) : (
                <code style={{ background: "var(--bg-elev-2)", padding: "2px 8px", borderRadius: 3 }}>
                  {project.default_branch}
                </code>
              )}
            </Row>

            <Row label="可见性">
              {editing ? (
                <select
                  className="input w-32"
                  value={draftVis}
                  onChange={(e) => setDraftVis(e.target.value as "private" | "public")}
                >
                  <option value="private">私有</option>
                  <option value="public">公开</option>
                </select>
              ) : (
                <span>{project.visibility === "public" ? "公开" : "私有"}</span>
              )}
            </Row>

            <div
              className="mt-4 p-4 rounded-md text-xs"
              style={{ background: "var(--bg-elev-2)", border: "1px dashed var(--border-strong)", color: "var(--fg-dim)" }}
            >
              ⓘ Webhook、流水线、运行记录在后续 milestone (M1.2 起) 接入。
            </div>
          </div>
        )}
      </div>
    </AppShell>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[100px_1fr] gap-3 items-start text-sm">
      <div style={{ color: "var(--fg-dim)" }}>{label}</div>
      <div>{children}</div>
    </div>
  );
}

function fmt(s: string): string {
  try { return new Date(s).toLocaleString(); } catch { return s; }
}
