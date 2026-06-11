"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { listClusters, type Cluster } from "@/lib/clusters-api";

const STATUS_COLOR: Record<string, string> = {
  healthy: "var(--success)",
  degraded: "var(--warn)",
  unhealthy: "var(--danger)",
  disconnected: "var(--danger)",
  unknown: "var(--fg-dim)",
};

export default function ClustersPage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listClusters(token)
      .then(setClusters)
      .catch(() => setClusters([]))
      .finally(() => setLoading(false));
  }, [token]);

  return (
    <AppShell title="集群">
      <div style={{ padding: 24 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
          <h1 style={{ fontSize: 18, fontWeight: 590, margin: 0 }}>集群列表</h1>
          <button className="btn btn-primary" onClick={() => router.push("/clusters/new")}>
            + 接入集群
          </button>
        </div>

        {loading && <div style={{ color: "var(--fg-dim)" }}>加载中…</div>}

        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(300px, 1fr))", gap: 16 }}>
          {clusters.map((c) => (
            <div
              key={c.id}
              className="card"
              style={{ cursor: "pointer" }}
              onClick={() => router.push(`/clusters/${c.id}`)}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 12 }}>
                <div
                  style={{
                    width: 32, height: 32, borderRadius: 6,
                    background: "var(--accent-soft)", color: "var(--accent)",
                    display: "flex", alignItems: "center", justifyContent: "center", fontSize: 14,
                  }}
                >
                  ☸
                </div>
                <div>
                  <div style={{ fontSize: 14, fontWeight: 590, color: "var(--fg)" }}>{c.name}</div>
                  <div style={{ fontSize: 12, color: "var(--fg-dim)", textTransform: "uppercase" }}>{c.provider}</div>
                </div>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <span
                  style={{
                    width: 8, height: 8, borderRadius: "50%",
                    background: STATUS_COLOR[c.status] || "var(--fg-dim)",
                    display: "inline-block",
                  }}
                />
                <span style={{ fontSize: 12, color: "var(--fg-mute)" }}>{c.status}</span>
              </div>
            </div>
          ))}
        </div>

        {!loading && clusters.length === 0 && (
          <div style={{ color: "var(--fg-dim)", textAlign: "center", padding: 48 }}>
            暂无集群，点击右上角"接入集群"开始
          </div>
        )}
      </div>
    </AppShell>
  );
}
