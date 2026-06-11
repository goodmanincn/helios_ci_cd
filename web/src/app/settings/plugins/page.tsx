"use client";

// 已安装插件管理页 (M9).  /settings/plugins
// 列已安装 + 卸载 (二次确认) + 跳详情.

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { listInstalled, uninstallPlugin, type InstalledPlugin } from "@/lib/marketplace";

export default function InstalledPluginsPage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [list, setList] = useState<InstalledPlugin[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string>("");

  const reload = () => {
    setLoading(true);
    listInstalled(token)
      .then(setList)
      .catch(() => setList([]))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  const onUninstall = async (slug: string) => {
    if (!confirm(`确认卸载 ${slug} 吗?\n该 org 内所有引用该插件的流水线将无法继续运行 (直到重新安装).`)) return;
    setBusy(slug);
    try {
      await uninstallPlugin(token, slug);
      reload();
    } finally {
      setBusy("");
    }
  };

  return (
    <AppShell title="已安装插件">
      <div style={{ padding: 24, maxWidth: 960, margin: "0 auto" }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <h1 style={{ fontSize: 18, fontWeight: 590, margin: 0 }}>已安装插件</h1>
            <span style={{ fontSize: 12, color: "var(--fg-dim)" }}>{list.length} 个</span>
          </div>
          <button className="btn btn-primary" onClick={() => router.push("/plugins")}>
            浏览市场
          </button>
        </div>

        {loading ? (
          <div style={{ padding: 48, color: "var(--fg-dim)", textAlign: "center" }}>加载中…</div>
        ) : list.length === 0 ? (
          <div
            style={{
              padding: 48,
              color: "var(--fg-dim)",
              textAlign: "center",
              border: "1px dashed var(--border)",
              borderRadius: 8,
            }}
          >
            还没有装任何插件 — 前往
            <button
              onClick={() => router.push("/plugins")}
              style={{ background: "none", border: "none", color: "var(--accent)", cursor: "pointer", padding: "0 4px" }}
            >
              插件市场
            </button>
            浏览
          </div>
        ) : (
          <div style={{ border: "1px solid var(--border)", borderRadius: 8, overflow: "hidden" }}>
            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
              <thead style={{ background: "var(--surface)" }}>
                <tr>
                  <th style={th}>插件</th>
                  <th style={th}>版本</th>
                  <th style={th}>已安装</th>
                  <th style={{ ...th, textAlign: "right" }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {list.map((it) => {
                  const slug = `${it.plugin.namespace}/${it.plugin.name}`;
                  return (
                    <tr key={it.installation.id} style={{ borderTop: "1px solid var(--border)" }}>
                      <td style={td}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                          <span style={{ fontSize: 18 }}>🧩</span>
                          <div>
                            <div style={{ color: "var(--fg)", fontWeight: 510 }}>{slug}</div>
                            {it.plugin.description && (
                              <div style={{ fontSize: 11, color: "var(--fg-dim)", marginTop: 2 }}>
                                {it.plugin.description}
                              </div>
                            )}
                          </div>
                          {it.plugin.verified && (
                            <span
                              style={{
                                fontSize: 10,
                                padding: "1px 6px",
                                borderRadius: 3,
                                background: "rgba(34,197,94,0.12)",
                                color: "var(--success, #22c55e)",
                                border: "1px solid rgba(34,197,94,0.3)",
                              }}
                            >
                              ✓
                            </span>
                          )}
                        </div>
                      </td>
                      <td style={{ ...td, fontFamily: "var(--font-mono)", fontSize: 12 }}>
                        {it.version.version}
                        {it.plugin.latest_version && it.plugin.latest_version !== it.version.version && (
                          <span style={{ marginLeft: 8, color: "var(--warn, #f59e0b)" }}>
                            (latest: {it.plugin.latest_version})
                          </span>
                        )}
                      </td>
                      <td style={{ ...td, color: "var(--fg-dim)", fontSize: 12 }}>
                        {new Date(it.installation.installed_at).toLocaleString()}
                      </td>
                      <td style={{ ...td, textAlign: "right" }}>
                        <button
                          className="btn"
                          style={{ marginRight: 6, padding: "3px 10px", fontSize: 12 }}
                          onClick={() => router.push(`/plugins/${slug}`)}
                        >
                          详情
                        </button>
                        <button
                          className="btn"
                          style={{
                            padding: "3px 10px",
                            fontSize: 12,
                            color: "var(--danger, #ef4444)",
                            borderColor: "var(--danger, #ef4444)",
                          }}
                          disabled={busy === slug}
                          onClick={() => onUninstall(slug)}
                        >
                          {busy === slug ? "卸载中…" : "卸载"}
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </AppShell>
  );
}

const th: React.CSSProperties = {
  textAlign: "left",
  padding: "10px 12px",
  color: "var(--fg-dim)",
  fontWeight: 510,
  fontSize: 12,
};

const td: React.CSSProperties = {
  padding: "12px 12px",
  color: "var(--fg-mute)",
};
