"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";

const DEMO_PIPELINES = [
  { id: 1, name: "build-and-deploy", project: "api-gateway", enabled: true },
  { id: 2, name: "test-matrix", project: "web-frontend", enabled: true },
  { id: 3, name: "deploy-staging", project: "infra", enabled: false },
];

export default function PipelinesPage() {
  const router = useRouter();
  const [inputId, setInputId] = useState("");

  return (
    <AppShell title="流水线">
      <div style={{ padding: 24 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 24 }}>
          <input
            type="text"
            placeholder="输入 Pipeline ID 直接编辑…"
            className="input"
            style={{ maxWidth: 300, fontSize: 13 }}
            value={inputId}
            onChange={(e) => setInputId(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && inputId.trim()) {
                router.push(`/pipelines/${inputId.trim()}/edit`);
              }
            }}
          />
          <button
            className="btn btn-primary"
            disabled={!inputId.trim()}
            onClick={() => router.push(`/pipelines/${inputId.trim()}/edit`)}
          >
            进入编辑器
          </button>
        </div>

        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
            gap: 16,
          }}
        >
          {DEMO_PIPELINES.map((p) => (
            <div
              key={p.id}
              className="card"
              style={{ cursor: "pointer" }}
              onClick={() => router.push(`/pipelines/${p.id}/edit`)}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 8 }}>
                <div
                  style={{
                    width: 32,
                    height: 32,
                    borderRadius: 6,
                    background: "var(--accent-soft)",
                    color: "var(--accent)",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    fontSize: 14,
                  }}
                >
                  🔀
                </div>
                <div>
                  <div style={{ fontSize: 14, fontWeight: 590, color: "var(--fg)" }}>{p.name}</div>
                  <div style={{ fontSize: 12, color: "var(--fg-dim)" }}>{p.project}</div>
                </div>
              </div>
              <div style={{ fontSize: 11, color: "var(--fg-dim)" }}>
                {p.enabled ? (
                  <span style={{ color: "var(--success)" }}>● 已启用</span>
                ) : (
                  <span>○ 已停用</span>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>
    </AppShell>
  );
}
