"use client";

// 流水线模板市场 (M8 T8.2.1).
import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { listTemplates, type PipelineTemplate } from "@/lib/templates-api";

const CATEGORY_LABELS: Record<string, string> = {
  fullstack: "全栈",
  release: "发布",
  deploy: "部署",
  build: "构建",
};

export default function TemplatesMarketplacePage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [templates, setTemplates] = useState<PipelineTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [q, setQ] = useState("");
  const [category, setCategory] = useState("");

  useEffect(() => {
    listTemplates(token)
      .then(setTemplates)
      .catch(() => setTemplates([]))
      .finally(() => setLoading(false));
  }, [token]);

  const categories = useMemo(() => {
    const set = new Set<string>();
    templates.forEach((t) => t.category && set.add(t.category));
    return Array.from(set).sort();
  }, [templates]);

  const filtered = useMemo(() => {
    return templates.filter((t) => {
      if (category && t.category !== category) return false;
      if (q) {
        const needle = q.toLowerCase();
        return (
          t.slug.toLowerCase().includes(needle) ||
          t.name.toLowerCase().includes(needle) ||
          (t.description || "").toLowerCase().includes(needle) ||
          (t.tags || []).some((tag) => tag.toLowerCase().includes(needle))
        );
      }
      return true;
    });
  }, [templates, q, category]);

  return (
    <AppShell title="模板市场">
      <div style={{ padding: 24 }}>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            marginBottom: 24,
            gap: 12,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <h1 style={{ fontSize: 18, fontWeight: 590, margin: 0 }}>流水线模板</h1>
            <span style={{ fontSize: 12, color: "var(--fg-dim)" }}>{filtered.length} 个</span>
          </div>
          <div style={{ display: "flex", gap: 8 }}>
            <input
              className="input"
              placeholder="搜索模板"
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
                  {CATEGORY_LABELS[c] || c}
                </option>
              ))}
            </select>
          </div>
        </div>

        {loading ? (
          <div style={{ color: "var(--fg-dim)", padding: 48, textAlign: "center" }}>加载中…</div>
        ) : filtered.length === 0 ? (
          <div style={{ color: "var(--fg-dim)", padding: 48, textAlign: "center" }}>没有匹配的模板</div>
        ) : (
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
              gap: 12,
            }}
          >
            {filtered.map((t) => (
              <TemplateCard key={t.id} t={t} onOpen={() => router.push(`/templates/${t.slug}`)} />
            ))}
          </div>
        )}
      </div>
    </AppShell>
  );
}

function TemplateCard({ t, onOpen }: { t: PipelineTemplate; onOpen: () => void }) {
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
          📋
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 14,
              fontWeight: 590,
              color: "var(--fg)",
              display: "flex",
              gap: 6,
              alignItems: "center",
            }}
          >
            <span>{t.name}</span>
            {t.builtin && (
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
                内置
              </span>
            )}
          </div>
          <div style={{ fontSize: 11, color: "var(--fg-dim)", fontFamily: "var(--font-mono)" }}>
            {t.slug}
          </div>
        </div>
      </div>

      <div style={{ fontSize: 12, color: "var(--fg-mute)", flex: 1, lineHeight: 1.5 }}>
        {t.description || <span style={{ color: "var(--fg-dim)" }}>无描述</span>}
      </div>

      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          fontSize: 11,
          color: "var(--fg-dim)",
        }}
      >
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {t.category && (
            <span style={{ padding: "1px 6px", borderRadius: 3, background: "rgba(255,255,255,0.05)" }}>
              {CATEGORY_LABELS[t.category] || t.category}
            </span>
          )}
          {(t.tags || []).slice(0, 3).map((tag) => (
            <span
              key={tag}
              style={{
                padding: "1px 6px",
                borderRadius: 3,
                background: "rgba(255,255,255,0.03)",
                fontFamily: "var(--font-mono)",
              }}
            >
              {tag}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}
