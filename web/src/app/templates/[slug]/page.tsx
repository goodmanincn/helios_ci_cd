"use client";

// 流水线模板详情 + 一键克隆 (M8 T8.2.1).
import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { listProjects, type Project } from "@/lib/projects-api";
import {
  cloneFromTemplate,
  listTemplates,
  type PipelineTemplate,
} from "@/lib/templates-api";

export default function TemplateDetailPage() {
  const params = useParams() as { slug: string };
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const slug = params.slug;

  const [template, setTemplate] = useState<PipelineTemplate | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectId, setProjectId] = useState("");
  const [pipelineName, setPipelineName] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [showClone, setShowClone] = useState(false);

  useEffect(() => {
    setLoading(true);
    listTemplates(token, { q: slug })
      .then((list) => {
        const found = list.find((t) => t.slug === slug);
        if (!found) {
          setError("模板不存在");
          setTemplate(null);
          return;
        }
        setTemplate(found);
        setPipelineName(found.slug);
      })
      .catch((e) => setError(String((e as Error)?.message || e)))
      .finally(() => setLoading(false));
  }, [slug, token]);

  useEffect(() => {
    if (!token) return;
    listProjects(token, { limit: 100 })
      .then((r) => setProjects(r.items))
      .catch(() => setProjects([]));
  }, [token]);

  const onClone = async () => {
    if (!template || !projectId || !pipelineName.trim()) return;
    setBusy(true);
    setError("");
    try {
      const res = await cloneFromTemplate(token, {
        template_slug: template.slug,
        project_id: Number(projectId),
        name: pipelineName.trim(),
      });
      router.push(`/pipelines/${res.pipeline_id}/edit`);
    } catch (e) {
      setError(String((e as Error)?.message || e));
    } finally {
      setBusy(false);
    }
  };

  if (loading) {
    return (
      <AppShell title={slug}>
        <div style={{ padding: 48, color: "var(--fg-dim)", textAlign: "center" }}>加载中…</div>
      </AppShell>
    );
  }

  if (!template) {
    return (
      <AppShell title={slug}>
        <div style={{ padding: 48, color: "var(--fg-dim)", textAlign: "center" }}>
          {error || "未找到模板"}
        </div>
      </AppShell>
    );
  }

  return (
    <AppShell
      title={template.name}
      actions={
        <button className="btn btn-primary" onClick={() => setShowClone(true)} disabled={busy}>
          克隆到项目
        </button>
      }
    >
      <div style={{ padding: 24, maxWidth: 960 }}>
        {error && (
          <div
            style={{
              marginBottom: 16,
              padding: 12,
              borderRadius: 6,
              background: "rgba(239,68,68,0.1)",
              border: "1px solid rgba(239,68,68,0.3)",
              color: "#f87171",
              fontSize: 13,
            }}
          >
            {error}
          </div>
        )}

        <div style={{ marginBottom: 24 }}>
          <div style={{ fontSize: 13, color: "var(--fg-dim)", marginBottom: 8 }}>
            <span style={{ fontFamily: "var(--font-mono)" }}>{template.slug}</span>
            {template.builtin && (
              <span style={{ marginLeft: 8, color: "var(--success)" }}>· 内置模板</span>
            )}
          </div>
          <p style={{ fontSize: 14, color: "var(--fg-mute)", lineHeight: 1.6, margin: 0 }}>
            {template.description || "无描述"}
          </p>
          <div style={{ display: "flex", gap: 8, marginTop: 12, flexWrap: "wrap" }}>
            {template.category && (
              <span
                style={{
                  fontSize: 11,
                  padding: "2px 8px",
                  borderRadius: 4,
                  background: "rgba(255,255,255,0.05)",
                  color: "var(--fg-dim)",
                }}
              >
                {template.category}
              </span>
            )}
            {(template.tags || []).map((tag) => (
              <span
                key={tag}
                style={{
                  fontSize: 11,
                  padding: "2px 8px",
                  borderRadius: 4,
                  background: "rgba(255,255,255,0.03)",
                  color: "var(--fg-dim)",
                  fontFamily: "var(--font-mono)",
                }}
              >
                {tag}
              </span>
            ))}
          </div>
        </div>

        <div>
          <div style={{ fontSize: 13, fontWeight: 590, marginBottom: 8, color: "var(--fg)" }}>流水线 YAML</div>
          <pre
            style={{
              background: "var(--surface)",
              border: "1px solid var(--border)",
              borderRadius: 8,
              padding: 16,
              fontSize: 12,
              lineHeight: 1.5,
              overflow: "auto",
              maxHeight: 480,
              fontFamily: "var(--font-mono)",
              color: "var(--fg-mute)",
            }}
          >
            {template.spec_raw || "（无 spec_raw）"}
          </pre>
        </div>
      </div>

      {showClone && (
        <div
          style={{
            position: "fixed",
            inset: 0,
            background: "rgba(0,0,0,0.6)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            zIndex: 100,
          }}
          onClick={() => !busy && setShowClone(false)}
        >
          <div
            className="card"
            style={{ width: 420, padding: 24 }}
            onClick={(e) => e.stopPropagation()}
          >
            <h2 style={{ fontSize: 16, fontWeight: 590, margin: "0 0 16px" }}>克隆模板</h2>
            <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
              <label style={{ fontSize: 12, color: "var(--fg-dim)" }}>
                目标项目
                <select
                  className="input"
                  style={{ marginTop: 4, width: "100%" }}
                  value={projectId}
                  onChange={(e) => setProjectId(e.target.value)}
                >
                  <option value="">选择项目…</option>
                  {projects.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} ({p.slug})
                    </option>
                  ))}
                </select>
              </label>
              <label style={{ fontSize: 12, color: "var(--fg-dim)" }}>
                流水线名称
                <input
                  className="input"
                  style={{ marginTop: 4, width: "100%" }}
                  value={pipelineName}
                  onChange={(e) => setPipelineName(e.target.value)}
                />
              </label>
            </div>
            <div style={{ display: "flex", gap: 8, marginTop: 20, justifyContent: "flex-end" }}>
              <button className="btn" onClick={() => setShowClone(false)} disabled={busy}>
                取消
              </button>
              <button
                className="btn btn-primary"
                onClick={onClone}
                disabled={busy || !projectId || !pipelineName.trim()}
              >
                {busy ? "克隆中…" : "确认克隆"}
              </button>
            </div>
          </div>
        </div>
      )}
    </AppShell>
  );
}
