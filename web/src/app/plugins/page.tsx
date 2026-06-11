"use client";

// 插件市场列表页 (M9).
// dark theme, 卡片网格 + 搜索 + verified 徽章.

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { listPlugins, type Plugin } from "@/lib/marketplace";

export default function PluginsMarketplacePage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [loading, setLoading] = useState(true);
  const [q, setQ] = useState("");
  const [category, setCategory] = useState<string>("");

  useEffect(() => {
    listPlugins(token)
      .then(setPlugins)
      .catch(() => setPlugins([]))
      .finally(() => setLoading(false));
  }, [token]);

  const categories = useMemo(() => {
    const set = new Set<string>();
    plugins.forEach((p) => p.category && set.add(p.category));
    return Array.from(set).sort();
  }, [plugins]);

  const filtered = useMemo(() => {
    return plugins.filter((p) => {
      if (category && p.category !== category) return false;
      if (q) {
        const t = q.toLowerCase();
        return (
          p.namespace.toLowerCase().includes(t) ||
          p.name.toLowerCase().includes(t) ||
          (p.description || "").toLowerCase().includes(t)
        );
      }
      return true;
    });
  }, [plugins, q, category]);

  return (
    <AppShell title="插件市场">
      <div style={{ padding: 24 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24, gap: 12 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <h1 style={{ fontSize: 18, fontWeight: 590, margin: 0 }}>插件市场</h1>
            <span style={{ fontSize: 12, color: "var(--fg-dim)" }}>{filtered.length} 个</span>
          </div>
          <div style={{ display: "flex", gap: 8 }}>
            <input
              className="input"
              placeholder="搜索插件"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              style={{ width: 220, fontSize: 12 }}
            />
            <select
              className="input"
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              style={{ width: 140, fontSize: 12 }}
            >
              <option value="">全部分类</option>
              {categories.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
            <button className="btn" onClick={() => router.push("/settings/plugins")}>
              已安装
            </button>
          </div>
        </div>

        {loading ? (
          <div style={{ color: "var(--fg-dim)", padding: 48, textAlign: "center" }}>加载中…</div>
        ) : filtered.length === 0 ? (
          <div style={{ color: "var(--fg-dim)", padding: 48, textAlign: "center" }}>没有匹配的插件</div>
        ) : (
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
              gap: 12,
            }}
          >
            {filtered.map((p) => (
              <PluginCard key={p.id} p={p} onOpen={() => router.push(`/plugins/${p.namespace}/${p.name}`)} />
            ))}
          </div>
        )}
      </div>
    </AppShell>
  );
}

function PluginCard({ p, onOpen }: { p: Plugin; onOpen: () => void }) {
  return (
    <div
      onClick={onOpen}
      style={{
        background: "var(--surface)",
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: 16,
        cursor: "pointer",
        transition: "border-color 0.15s",
        display: "flex",
        flexDirection: "column",
        gap: 8,
        minHeight: 140,
      }}
      onMouseEnter={(e) => (e.currentTarget.style.borderColor = "var(--accent)")}
      onMouseLeave={(e) => (e.currentTarget.style.borderColor = "var(--border)")}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <div
          style={{
            width: 32,
            height: 32,
            borderRadius: 6,
            background: "rgba(255,255,255,0.05)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: 16,
          }}
        >
          🧩
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 590, color: "var(--fg)", display: "flex", gap: 6, alignItems: "center" }}>
            <span>
              {p.namespace}/{p.name}
            </span>
            {p.verified && (
              <span
                title="官方/已审核"
                style={{
                  fontSize: 10,
                  padding: "1px 6px",
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
          {p.publisher && <div style={{ fontSize: 11, color: "var(--fg-dim)" }}>{p.publisher}</div>}
        </div>
      </div>

      <div style={{ fontSize: 12, color: "var(--fg-mute)", flex: 1, lineHeight: 1.5 }}>
        {p.description || <span style={{ color: "var(--fg-dim)" }}>无描述</span>}
      </div>

      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", fontSize: 11, color: "var(--fg-dim)" }}>
        <div style={{ display: "flex", gap: 8 }}>
          {p.category && (
            <span style={{ padding: "1px 6px", borderRadius: 3, background: "rgba(255,255,255,0.05)" }}>{p.category}</span>
          )}
          {p.latest_version && <span style={{ fontFamily: "var(--font-mono)" }}>{p.latest_version}</span>}
        </div>
        <span>↓ {p.downloads}</span>
      </div>
    </div>
  );
}
