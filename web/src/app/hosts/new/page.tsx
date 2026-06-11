"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { createHost } from "@/lib/hosts-api";

export default function NewHostPage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [name, setName] = useState("");
  const [ip, setIp] = useState("");
  const [sshPort, setSshPort] = useState(22);
  const [sshUser, setSshUser] = useState("root");
  const [labels, setLabels] = useState("");
  const [saving, setSaving] = useState(false);

  async function handleSubmit() {
    if (!name.trim() || !ip.trim()) return;
    setSaving(true);
    try {
      const labelObj: Record<string, string> = {};
      labels.split(",").forEach((pair) => {
        const [k, v] = pair.trim().split("=");
        if (k) labelObj[k] = v || "";
      });
      await createHost(token, {
        name: name.trim(),
        ip: ip.trim(),
        ssh_port: sshPort,
        ssh_user: sshUser.trim(),
        labels: labelObj,
      });
      router.push("/hosts");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "创建失败";
      alert(msg);
    } finally {
      setSaving(false);
    }
  }

  return (
    <AppShell title="添加主机">
      <div style={{ maxWidth: 640, margin: "0 auto", padding: 24 }}>
        <h1 style={{ fontSize: 18, fontWeight: 590, marginBottom: 24 }}>添加新主机</h1>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            主机名称
          </label>
          <input type="text" className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="web-prod-01" />
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            IP 地址
          </label>
          <input type="text" className="input" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="192.168.1.10" />
        </div>

        <div style={{ display: "flex", gap: 16, marginBottom: 16 }}>
          <div style={{ flex: 1 }}>
            <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
              SSH 端口
            </label>
            <input type="number" className="input" value={sshPort} onChange={(e) => setSshPort(Number(e.target.value))} />
          </div>
          <div style={{ flex: 1 }}>
            <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
              SSH 用户
            </label>
            <input type="text" className="input" value={sshUser} onChange={(e) => setSshUser(e.target.value)} placeholder="root" />
          </div>
        </div>

        <div style={{ marginBottom: 24 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            标签 (可选, 逗号分隔, 如 env=prod,region=bj)
          </label>
          <input type="text" className="input" value={labels} onChange={(e) => setLabels(e.target.value)} placeholder="env=prod,region=bj" />
        </div>

        <div style={{ display: "flex", gap: 12 }}>
          <button className="btn" onClick={() => router.push("/hosts")}>取消</button>
          <button className="btn btn-primary" onClick={handleSubmit} disabled={saving || !name.trim() || !ip.trim()}>
            {saving ? "保存中…" : "保存"}
          </button>
        </div>
      </div>
    </AppShell>
  );
}
