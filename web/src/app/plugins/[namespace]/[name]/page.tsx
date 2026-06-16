"use client";

// 插件详情页 (M9). /plugins/<namespace>/<name>
// 显示 README + inputs/outputs 表格 + 版本下拉 + 安装/卸载.

import { useEffect, useMemo, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { marked } from "marked";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import {
  getPlugin,
  installPlugin,
  uninstallPlugin,
  type PluginDetail,
} from "@/lib/marketplace";

interface ActionSpec {
  name?: string;
  description?: string;
  inputs?: Record<
    string,
    { description?: string; required?: boolean; default?: unknown; type?: string }
  >;
  outputs?: Record<string, { description?: string }>;
  runs?: { using?: string; image?: string };
  needs_secrets?: string[];
  needs_permissions?: string[];
}

export default function PluginDetailPage() {
  const params = useParams() as { namespace: string; name: string };
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const slug = `${params.namespace}/${params.name}`;

  const [detail, setDetail] = useState<PluginDetail | null>(null);
  const [selectedVersion, setSelectedVersion] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>("");

  const reload = () => {
    setLoading(true);
    getPlugin(token, slug)
      .then((d) => {
        setDetail(d);
        if (d.versions.length && !selectedVersion) {
          setSelectedVersion(d.versions[0].version);
        }
      })
      .catch((e) => setError(String(e?.message || e)))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slug, token]);

  const onInstall = async () => {
    if (!detail) return;
    setBusy(true);
    try {
      await installPlugin(token, slug, selectedVersion);
      reload();
    } catch (e) {
      setError(String((e as Error)?.message || e));
    } finally {
      setBusy(false);
    }
  };

  const onUninstall = async () => {
    if (!detail) return;
    if (!confirm(`确认卸载 ${slug} 吗?`)) return;
    setBusy(true);
    try {
      await uninstallPlugin(token, slug);
      reload();
    } catch (e) {
      setError(String((e as Error)?.message || e));
    } finally {
      setBusy(false);
    }
  };

  if (loading)
    return (
      <AppShell title={slug}>
        <div style={{ padding: 48, color: "var(--fg-dim)", textAlign: "center" }}>加载中…</div>
      </AppShell>
    );
  if (!detail)
    return (
      <AppShell title={slug}>
        <div style={{ padding: 48, color: "var(--fg-dim)", textAlign: "center" }}>未找到插件: {error}</div>
      </AppShell>
    );

  const { plugin: p, versions, installed, installed_version } = detail;
  const selVer = versions.find((v) => v.version === selectedVersion) || versions[0];
  const spec = (selVer?.action_spec || {}) as ActionSpec;

  return (
    <AppShell title={slug}>
      <div style={{ padding: 24, maxWidth: 960, margin: "0 auto" }}>
        <button
          onClick={() => router.push("/plugins")}
          style={{
            background: "none",
            border: "none",
            color: "var(--fg-dim)",
            cursor: "pointer",
            fontSize: 12,
            marginBottom: 16,
            padding: 0,
          }}
        >
          ← 返回市场
        </button>

        {/* Header */}
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "flex-start",
            gap: 16,
            marginBottom: 24,
            paddingBottom: 16,
            borderBottom: "1px solid var(--border)",
          }}
        >
          <div style={{ display: "flex", gap: 16 }}>
            <div
              style={{
                width: 56,
                height: 56,
                borderRadius: 10,
                background: "rgba(255,255,255,0.05)",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                fontSize: 28,
              }}
            >
              🧩
            </div>
            <div>
              <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                <h1 style={{ fontSize: 20, fontWeight: 590, margin: 0 }}>{p.namespace}/{p.name}</h1>
                {p.verified && (
                  <span
                    style={{
                      fontSize: 11,
                      padding: "1px 8px",
                      borderRadius: 3,
                      background: "rgba(34,197,94,0.12)",
                      color: "var(--success, #22c55e)",
                      border: "1px solid rgba(34,197,94,0.3)",
                    }}
                  >
                    ✓ 已验证
                  </span>
                )}
              </div>
              <div style={{ fontSize: 13, color: "var(--fg-mute)", marginTop: 4 }}>{p.description}</div>
              <div style={{ fontSize: 12, color: "var(--fg-dim)", marginTop: 8, display: "flex", gap: 12 }}>
                {p.publisher && <span>by {p.publisher}</span>}
                {p.category && <span>· {p.category}</span>}
                <span>· ↓ {p.downloads}</span>
              </div>
            </div>
          </div>

          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <select
              className="input"
              value={selectedVersion}
              onChange={(e) => setSelectedVersion(e.target.value)}
              style={{ width: 100, fontSize: 12 }}
            >
              {versions.map((v) => (
                <option key={v.id} value={v.version}>
                  {v.version}
                  {v.is_latest ? " (latest)" : ""}
                </option>
              ))}
            </select>
            {installed ? (
              <>
                <span style={{ fontSize: 12, color: "var(--success, #22c55e)" }}>
                  已安装 {installed_version}
                </span>
                <button className="btn" disabled={busy} onClick={onUninstall}>
                  卸载
                </button>
                {installed_version !== selectedVersion && (
                  <button className="btn btn-primary" disabled={busy} onClick={onInstall}>
                    切换到 {selectedVersion}
                  </button>
                )}
              </>
            ) : (
              <button className="btn btn-primary" disabled={busy} onClick={onInstall}>
                安装
              </button>
            )}
          </div>
        </div>

        {error && (
          <div
            style={{
              padding: 12,
              borderRadius: 6,
              background: "rgba(239,68,68,0.1)",
              border: "1px solid rgba(239,68,68,0.3)",
              color: "var(--danger, #ef4444)",
              fontSize: 12,
              marginBottom: 16,
            }}
          >
            {error}
          </div>
        )}

        {/* 权限请求 (本轮仅展示) */}
        {spec.needs_permissions && spec.needs_permissions.length > 0 && (
          <Section title="权限">
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
              {spec.needs_permissions.map((perm) => (
                <span
                  key={perm}
                  style={{
                    fontSize: 11,
                    padding: "2px 8px",
                    borderRadius: 3,
                    background: "rgba(245,158,11,0.1)",
                    color: "var(--warn, #f59e0b)",
                    border: "1px solid rgba(245,158,11,0.3)",
                  }}
                >
                  {perm}
                </span>
              ))}
            </div>
          </Section>
        )}

        {spec.needs_secrets && spec.needs_secrets.length > 0 && (
          <Section title="需要的 Secrets">
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
              {spec.needs_secrets.map((s) => (
                <code
                  key={s}
                  style={{
                    fontSize: 12,
                    padding: "2px 8px",
                    borderRadius: 3,
                    background: "rgba(255,255,255,0.05)",
                    color: "var(--fg-mute)",
                  }}
                >
                  {s}
                </code>
              ))}
            </div>
          </Section>
        )}

        {/* Inputs */}
        {spec.inputs && Object.keys(spec.inputs).length > 0 && (
          <Section title="Inputs">
            <SimpleTable
              cols={["name", "type", "required", "default", "description"]}
              rows={Object.entries(spec.inputs).map(([k, v]) => ({
                name: k,
                type: v.type || "string",
                required: v.required ? "✓" : "",
                default: v.default !== undefined ? String(v.default) : "",
                description: v.description || "",
              }))}
            />
          </Section>
        )}

        {/* Outputs */}
        {spec.outputs && Object.keys(spec.outputs).length > 0 && (
          <Section title="Outputs">
            <SimpleTable
              cols={["name", "description"]}
              rows={Object.entries(spec.outputs).map(([k, v]) => ({
                name: k,
                description: v.description || "",
              }))}
            />
          </Section>
        )}

        {/* README (markdown 渲染) */}
        {selVer?.readme && (
          <Section title="README">
            <ReadmeMarkdown raw={selVer.readme} />
          </Section>
        )}

        {/* action.yml */}
        <Section title="action.yml">
          <pre
            style={{
              margin: 0,
              padding: 16,
              background: "var(--surface)",
              border: "1px solid var(--border)",
              borderRadius: 6,
              fontSize: 11,
              color: "var(--fg-mute)",
              fontFamily: "var(--font-mono)",
              overflowX: "auto",
            }}
          >
            {selVer?.action_yml}
          </pre>
        </Section>
      </div>
    </AppShell>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 24 }}>
      <h2 style={{ fontSize: 13, fontWeight: 590, color: "var(--fg-dim)", margin: "0 0 8px", textTransform: "uppercase", letterSpacing: 0.5 }}>
        {title}
      </h2>
      {children}
    </div>
  );
}

function SimpleTable({ cols, rows }: { cols: string[]; rows: Record<string, string>[] }) {
  return (
    <div style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
      <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 12 }}>
        <thead style={{ background: "var(--surface)" }}>
          <tr>
            {cols.map((c) => (
              <th key={c} style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} style={{ borderTop: "1px solid var(--border)" }}>
              {cols.map((c) => (
                <td
                  key={c}
                  style={{
                    padding: "8px 12px",
                    color: c === "name" ? "var(--fg)" : "var(--fg-mute)",
                    fontFamily: c === "name" || c === "type" || c === "default" ? "var(--font-mono)" : undefined,
                    fontSize: c === "name" || c === "type" || c === "default" ? 11 : 12,
                  }}
                >
                  {r[c]}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ReadmeMarkdown — 把 markdown 文本用 marked 渲染为 HTML, dark theme 适配.
function ReadmeMarkdown({ raw }: { raw: string }) {
  const html = useMemo(() => {
    const result = marked.parse(raw, { async: false });
    return typeof result === "string" ? result : "";
  }, [raw]);

  return (
    <div
      className="readme-md"
      dangerouslySetInnerHTML={{ __html: html }}
      style={{
        padding: 20,
        background: "var(--surface)",
        border: "1px solid var(--border)",
        borderRadius: 6,
        fontSize: 14,
        color: "var(--fg-mute)",
        lineHeight: 1.7,
      }}
    />
  );
}
