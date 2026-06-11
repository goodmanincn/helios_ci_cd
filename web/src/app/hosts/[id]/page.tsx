"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { getHost, updateHost, deleteHost, testHost, type Host, type HostTestResult } from "@/lib/hosts-api";

export default function HostDetailPage() {
  const { id } = useParams();
  const hostId = String(id ?? "");
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [host, setHost] = useState<Host | null>(null);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(false);
  const [testResult, setTestResult] = useState<HostTestResult | null>(null);
  const [testing, setTesting] = useState(false);

  // edit fields
  const [name, setName] = useState("");
  const [ip, setIp] = useState("");
  const [sshPort, setSshPort] = useState(22);
  const [sshUser, setSshUser] = useState("root");
  const [labels, setLabels] = useState("");

  useEffect(() => {
    getHost(token, hostId)
      .then((h) => {
        setHost(h);
        setName(h.name);
        setIp(h.ip);
        setSshPort(h.ssh_port || 22);
        setSshUser(h.ssh_user || "root");
        setLabels(h.labels ? Object.entries(h.labels).map(([k, v]) => `${k}=${v}`).join(",") : "");
      })
      .catch(() => setHost(null))
      .finally(() => setLoading(false));
  }, [token, hostId]);

  async function handleTest() {
    setTesting(true);
    setTestResult(null);
    try {
      const res = await testHost(token, hostId);
      setTestResult(res);
      // refresh host status
      const h = await getHost(token, hostId);
      setHost(h);
    } catch (e) {
      setTestResult({ reachable: false, ssh_ok: false, message: e instanceof Error ? e.message : "测试失败" });
    } finally {
      setTesting(false);
    }
  }

  async function handleSave() {
    if (!host) return;
    const labelObj: Record<string, string> = {};
    labels.split(",").forEach((pair) => {
      const [k, v] = pair.trim().split("=");
      if (k) labelObj[k] = v || "";
    });
    try {
      const updated = await updateHost(token, hostId, {
        name: name.trim(),
        ip: ip.trim(),
        ssh_port: sshPort,
        ssh_user: sshUser.trim(),
        labels: labelObj,
      });
      setHost(updated);
      setEditing(false);
    } catch (e) {
      alert(e instanceof Error ? e.message : "保存失败");
    }
  }

  async function handleDelete() {
    if (!window.confirm("确定删除该主机？此操作不可恢复。")) return;
    try {
      await deleteHost(token, hostId);
      router.push("/hosts");
    } catch (e) {
      alert(e instanceof Error ? e.message : "删除失败");
    }
  }

  const statusColor =
    host?.status === "online"
      ? "var(--success)"
      : host?.status === "offline"
        ? "var(--danger)"
        : "var(--fg-dim)";

  if (loading) {
    return (
      <AppShell title="主机详情">
        <div style={{ padding: 24, color: "var(--fg-dim)" }}>加载中…</div>
      </AppShell>
    );
  }
  if (!host) {
    return (
      <AppShell title="主机详情">
        <div style={{ padding: 24, color: "var(--danger)" }}>主机不存在</div>
      </AppShell>
    );
  }

  return (
    <AppShell title={host.name}>
      <div style={{ padding: 24 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 24 }}>
          <div
            style={{
              width: 40, height: 40, borderRadius: 8,
              background: "var(--accent-soft)", color: "var(--accent)",
              display: "flex", alignItems: "center", justifyContent: "center", fontSize: 18,
            }}
          >
            🖥
          </div>
          <div>
            <div style={{ fontSize: 16, fontWeight: 590, color: "var(--fg)" }}>{host.name}</div>
            <div style={{ fontSize: 12, color: "var(--fg-dim)" }}>{host.ip} · {host.ssh_user || "root"}@{host.ssh_port || 22}</div>
          </div>
          <span
            style={{
              marginLeft: "auto",
              display: "inline-flex", alignItems: "center", gap: 6,
              fontSize: 11, padding: "2px 8px",
              background: `${statusColor}20`, color: statusColor,
              borderRadius: 4, border: `1px solid ${statusColor}40`,
            }}
          >
            <span style={{ width: 6, height: 6, borderRadius: "50%", background: statusColor, display: "inline-block" }} />
            {host.status}
          </span>
        </div>

        <div style={{ display: "flex", gap: 12, marginBottom: 24 }}>
          <button className="btn bsm" onClick={handleTest} disabled={testing}>
            {testing ? "测试中…" : "连通性测试"}
          </button>
          <button className="btn bsm" onClick={() => setEditing(!editing)}>
            {editing ? "取消编辑" : "编辑"}
          </button>
          <button className="btn bsm" style={{ color: "var(--danger)" }} onClick={handleDelete}>
            删除
          </button>
        </div>

        {testResult && (
          <div
            style={{
              padding: "8px 12px", borderRadius: 6, marginBottom: 24, fontSize: 12,
              background: testResult.reachable ? "rgba(52,211,153,0.1)" : "rgba(248,113,113,0.1)",
              color: testResult.reachable ? "var(--success)" : "var(--danger)",
              border: `1px solid ${testResult.reachable ? "rgba(52,211,153,0.25)" : "rgba(248,113,113,0.25)"}`,
            }}
          >
            {testResult.ssh_ok
              ? `SSH 连通 · ${testResult.uname || ""}`
              : testResult.message || "连通性测试失败"}
          </div>
        )}

        {editing ? (
          <div style={{ display: "flex", flexDirection: "column", gap: 16, maxWidth: 640 }}>
            <div>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>名称</label>
              <input type="text" className="input" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>IP</label>
              <input type="text" className="input" value={ip} onChange={(e) => setIp(e.target.value)} />
            </div>
            <div style={{ display: "flex", gap: 16 }}>
              <div style={{ flex: 1 }}>
                <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>SSH 端口</label>
                <input type="number" className="input" value={sshPort} onChange={(e) => setSshPort(Number(e.target.value))} />
              </div>
              <div style={{ flex: 1 }}>
                <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>SSH 用户</label>
                <input type="text" className="input" value={sshUser} onChange={(e) => setSshUser(e.target.value)} />
              </div>
            </div>
            <div>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>标签 (逗号分隔)</label>
              <input type="text" className="input" value={labels} onChange={(e) => setLabels(e.target.value)} placeholder="env=prod,region=bj" />
            </div>
            <div style={{ display: "flex", gap: 12 }}>
              <button className="btn" onClick={() => setEditing(false)}>取消</button>
              <button className="btn btn-primary" onClick={handleSave}>保存</button>
            </div>
          </div>
        ) : (
          <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16, fontSize: 13 }}>
            <div className="card" style={{ padding: 16 }}>
              <div style={{ color: "var(--fg-dim)", fontSize: 11, marginBottom: 4 }}>操作系统</div>
              <div style={{ color: "var(--fg)" }}>{host.os || "—"}</div>
            </div>
            <div className="card" style={{ padding: 16 }}>
              <div style={{ color: "var(--fg-dim)", fontSize: 11, marginBottom: 4 }}>架构</div>
              <div style={{ color: "var(--fg)" }}>{host.arch || "—"}</div>
            </div>
            <div className="card" style={{ padding: 16 }}>
              <div style={{ color: "var(--fg-dim)", fontSize: 11, marginBottom: 4 }}>上次心跳</div>
              <div style={{ color: "var(--fg)" }}>
                {host.last_heartbeat ? new Date(host.last_heartbeat).toLocaleString() : "—"}
              </div>
            </div>
            <div className="card" style={{ padding: 16 }}>
              <div style={{ color: "var(--fg-dim)", fontSize: 11, marginBottom: 4 }}>创建时间</div>
              <div style={{ color: "var(--fg)" }}>{new Date(host.created_at).toLocaleString()}</div>
            </div>
          </div>
        )}
      </div>
    </AppShell>
  );
}
