"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import {
  getCluster,
  listWorkloads,
  listEvents,
  getDeploymentHistory,
  type Cluster,
  type WorkloadInfo,
  type EventInfo,
  type RevisionInfo,
} from "@/lib/clusters-api";

type Tab = "workloads" | "events" | "history";

export default function ClusterDetailPage() {
  const { id } = useParams();
  const clusterId = String(id ?? "");
  const token = useAuthStore((s) => s.accessToken);
  const [cluster, setCluster] = useState<Cluster | null>(null);
  const [tab, setTab] = useState<Tab>("workloads");
  const [workloads, setWorkloads] = useState<WorkloadInfo[]>([]);
  const [events, setEvents] = useState<EventInfo[]>([]);
  const [history, setHistory] = useState<RevisionInfo[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getCluster(token, clusterId)
      .then(setCluster)
      .catch(() => setCluster(null))
      .finally(() => setLoading(false));
  }, [token, clusterId]);

  useEffect(() => {
    if (!cluster) return;
    if (tab === "workloads") {
      listWorkloads(token, clusterId).then(setWorkloads).catch(() => setWorkloads([]));
    } else if (tab === "events") {
      listEvents(token, clusterId, undefined, 50).then(setEvents).catch(() => setEvents([]));
    } else if (tab === "history") {
      getDeploymentHistory(token, clusterId, "app").then(setHistory).catch(() => setHistory([]));
    }
  }, [tab, token, clusterId, cluster]);

  if (loading) {
    return (
      <AppShell title="集群详情">
        <div style={{ padding: 24, color: "var(--fg-dim)" }}>加载中…</div>
      </AppShell>
    );
  }
  if (!cluster) {
    return (
      <AppShell title="集群详情">
        <div style={{ padding: 24, color: "var(--danger)" }}>集群不存在</div>
      </AppShell>
    );
  }

  const statusColor =
    cluster.status === "healthy"
      ? "var(--success)"
      : cluster.status === "degraded"
        ? "var(--warn)"
        : "var(--danger)";

  return (
    <AppShell title={cluster.name}>
      <div style={{ padding: 24 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 24 }}>
          <div
            style={{
              width: 40,
              height: 40,
              borderRadius: 8,
              background: "var(--accent-soft)",
              color: "var(--accent)",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              fontSize: 18,
            }}
          >
            ☸
          </div>
          <div>
            <div style={{ fontSize: 16, fontWeight: 590, color: "var(--fg)" }}>
              {cluster.name}
            </div>
            <div style={{ fontSize: 12, color: "var(--fg-dim)" }}>
              {cluster.provider} · {cluster.region || "—"}
            </div>
          </div>
          <span
            style={{
              marginLeft: "auto",
              display: "inline-flex",
              alignItems: "center",
              gap: 6,
              fontSize: 11,
              padding: "2px 8px",
              background: `${statusColor}20`,
              color: statusColor,
              borderRadius: 4,
              border: `1px solid ${statusColor}40`,
            }}
          >
            <span
              style={{
                width: 6,
                height: 6,
                borderRadius: "50%",
                background: statusColor,
                display: "inline-block",
              }}
            />
            {cluster.status}
          </span>
        </div>

        <div
          style={{
            display: "flex",
            gap: 2,
            background: "rgba(255,255,255,0.03)",
            border: "1px solid var(--border)",
            borderRadius: 6,
            padding: 2,
            marginBottom: 24,
            width: "fit-content",
          }}
        >
          {(["workloads", "events", "history"] as Tab[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className="btn bsm"
              style={{
                padding: "4px 12px",
                fontSize: 12,
                borderRadius: 5,
                border: "none",
                background: tab === t ? "rgba(94,106,210,0.15)" : "transparent",
                color: tab === t ? "var(--fg)" : "var(--fg-mute)",
              }}
            >
              {t === "workloads" ? "工作负载" : t === "events" ? "事件流" : "部署历史"}
            </button>
          ))}
        </div>

        {tab === "workloads" && (
          <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: "1px solid var(--border)" }}>
                <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>类型</th>
                <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>名称</th>
                <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>命名空间</th>
                <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>状态</th>
                <th style={{ textAlign: "right", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>Ready</th>
              </tr>
            </thead>
            <tbody>
              {workloads.map((w) => (
                <tr key={`${w.kind}-${w.name}`} style={{ borderBottom: "1px solid var(--border)" }}>
                  <td style={{ padding: "10px 12px", color: "var(--fg-mute)" }}>{w.kind}</td>
                  <td style={{ padding: "10px 12px", color: "var(--fg)" }}>{w.name}</td>
                  <td style={{ padding: "10px 12px", color: "var(--fg-dim)", fontSize: 12 }}>{w.namespace}</td>
                  <td style={{ padding: "10px 12px" }}>
                    <span
                      style={{
                        fontSize: 11,
                        padding: "2px 6px",
                        borderRadius: 4,
                        background:
                          w.status === "healthy"
                            ? "rgba(52,211,153,0.1)"
                            : w.status === "progressing"
                              ? "rgba(251,191,36,0.1)"
                              : "rgba(248,113,113,0.1)",
                        color:
                          w.status === "healthy"
                            ? "var(--success)"
                            : w.status === "progressing"
                              ? "var(--warn)"
                              : "var(--danger)",
                      }}
                    >
                      {w.status}
                    </span>
                  </td>
                  <td style={{ padding: "10px 12px", textAlign: "right", color: "var(--fg-mute)" }}>
                    {w.ready}/{w.desired}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {tab === "events" && (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            {events.map((e, i) => (
              <div
                key={i}
                style={{
                  padding: "10px 12px",
                  borderRadius: 6,
                  background: "var(--bg-elev)",
                  border: "1px solid var(--border)",
                  fontSize: 13,
                }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
                  <span
                    style={{
                      fontSize: 10,
                      padding: "1px 5px",
                      borderRadius: 3,
                      background:
                        e.type === "Warning"
                          ? "rgba(248,113,113,0.1)"
                          : "rgba(52,211,153,0.1)",
                      color: e.type === "Warning" ? "var(--danger)" : "var(--success)",
                    }}
                  >
                    {e.type}
                  </span>
                  <span style={{ fontWeight: 590, color: "var(--fg)" }}>{e.reason}</span>
                  <span style={{ marginLeft: "auto", fontSize: 11, color: "var(--fg-dim)" }}>
                    {new Date(e.timestamp).toLocaleString()}
                  </span>
                </div>
                <div style={{ color: "var(--fg-mute)", fontSize: 12 }}>{e.message}</div>
                <div style={{ color: "var(--fg-dim)", fontSize: 11, marginTop: 2 }}>{e.object}</div>
              </div>
            ))}
            {events.length === 0 && (
              <div style={{ color: "var(--fg-dim)", padding: 24, textAlign: "center" }}>暂无事件</div>
            )}
          </div>
        )}

        {tab === "history" && (
          <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: "1px solid var(--border)" }}>
                <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>Revision</th>
                <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>镜像</th>
                <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>状态</th>
                <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>时间</th>
              </tr>
            </thead>
            <tbody>
              {history.map((h) => (
                <tr key={h.revision} style={{ borderBottom: "1px solid var(--border)" }}>
                  <td style={{ padding: "10px 12px", color: "var(--fg)" }}>v{h.revision}</td>
                  <td style={{ padding: "10px 12px", color: "var(--fg-mute)", fontSize: 12, fontFamily: "var(--font-mono)" }}>
                    {h.image}
                  </td>
                  <td style={{ padding: "10px 12px" }}>
                    <span
                      style={{
                        fontSize: 11,
                        padding: "2px 6px",
                        borderRadius: 4,
                        background:
                          h.status === "healthy"
                            ? "rgba(52,211,153,0.1)"
                            : "rgba(251,191,36,0.1)",
                        color: h.status === "healthy" ? "var(--success)" : "var(--warn)",
                      }}
                    >
                      {h.status}
                    </span>
                  </td>
                  <td style={{ padding: "10px 12px", color: "var(--fg-dim)", fontSize: 12 }}>
                    {new Date(h.created_at).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </AppShell>
  );
}
