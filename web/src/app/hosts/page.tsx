"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { listHosts, type Host } from "@/lib/hosts-api";

const STATUS_COLOR: Record<string, string> = {
  online: "var(--success)",
  offline: "var(--danger)",
  unknown: "var(--fg-dim)",
};

export default function HostsPage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);
  const [q, setQ] = useState("");

  useEffect(() => {
    listHosts(token)
      .then(setHosts)
      .catch(() => setHosts([]))
      .finally(() => setLoading(false));
  }, [token]);

  const filtered = hosts.filter(
    (h) =>
      h.name.toLowerCase().includes(q.toLowerCase()) ||
      h.ip.includes(q),
  );

  return (
    <AppShell title="主机">
      <div style={{ padding: 24 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <h1 style={{ fontSize: 18, fontWeight: 590, margin: 0 }}>主机列表</h1>
            <input
              type="text"
              className="input"
              placeholder="搜索名称或 IP"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              style={{ width: 220, fontSize: 12 }}
            />
          </div>
          <button className="btn btn-primary" onClick={() => router.push("/hosts/new")}>
            + 添加主机
          </button>
        </div>

        {loading && <div style={{ color: "var(--fg-dim)" }}>加载中…</div>}

        <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: "1px solid var(--border)" }}>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>名称</th>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>IP</th>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>SSH</th>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>状态</th>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>标签</th>
              <th style={{ textAlign: "right", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((h) => (
              <tr
                key={h.id}
                style={{ borderBottom: "1px solid var(--border)", cursor: "pointer" }}
                onClick={() => router.push(`/hosts/${h.id}`)}
              >
                <td style={{ padding: "10px 12px", color: "var(--fg)", fontWeight: 510 }}>{h.name}</td>
                <td style={{ padding: "10px 12px", color: "var(--fg-mute)", fontFamily: "var(--font-mono)", fontSize: 12 }}>{h.ip}</td>
                <td style={{ padding: "10px 12px", color: "var(--fg-dim)", fontSize: 12 }}>{h.ssh_user || "root"}@{h.ssh_port || 22}</td>
                <td style={{ padding: "10px 12px" }}>
                  <span
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      gap: 6,
                      fontSize: 11,
                      padding: "2px 8px",
                      background: `${STATUS_COLOR[h.status] || "var(--fg-dim)"}20`,
                      color: STATUS_COLOR[h.status] || "var(--fg-dim)",
                      borderRadius: 4,
                      border: `1px solid ${STATUS_COLOR[h.status] || "var(--fg-dim)"}40`,
                    }}
                  >
                    <span
                      style={{
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        background: STATUS_COLOR[h.status] || "var(--fg-dim)",
                        display: "inline-block",
                      }}
                    />
                    {h.status}
                  </span>
                </td>
                <td style={{ padding: "10px 12px" }}>
                  <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                    {h.labels &&
                      Object.entries(h.labels).map(([k, v]) => (
                        <span
                          key={k}
                          style={{
                            fontSize: 10,
                            padding: "1px 5px",
                            borderRadius: 3,
                            background: "rgba(255,255,255,0.05)",
                            color: "var(--fg-dim)",
                          }}
                        >
                          {k}={v}
                        </span>
                      ))}
                  </div>
                </td>
                <td
                  style={{ padding: "10px 12px", textAlign: "right" }}
                  onClick={(e) => e.stopPropagation()}
                >
                  <button
                    className="btn bsm"
                    style={{ padding: "2px 8px", fontSize: 11 }}
                    onClick={() => router.push(`/hosts/${h.id}`)}
                  >
                    详情
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {!loading && filtered.length === 0 && (
          <div style={{ color: "var(--fg-dim)", textAlign: "center", padding: 48 }}>
            暂无主机，点击右上角"添加主机"开始
          </div>
        )}
      </div>
    </AppShell>
  );
}
