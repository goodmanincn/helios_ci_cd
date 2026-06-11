"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { createCluster, testCluster } from "@/lib/clusters-api";

export default function NewClusterPage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("selfhosted");
  const [region, setRegion] = useState("");
  const [kubeconfig, setKubeconfig] = useState("");
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  async function handleTest() {
    if (!kubeconfig.trim()) return;
    setTesting(true);
    setTestResult(null);
    try {
      const info = await testCluster(token, { provider, kubeconfig });
      setTestResult(`连接成功: ${info.version} · ${info.node_count} 节点 · ${info.namespace_count} 命名空间`);
    } catch (e) {
      const msg = e instanceof Error ? e.message : "测试失败";
      setTestResult(`失败: ${msg}`);
    } finally {
      setTesting(false);
    }
  }

  async function handleSubmit() {
    if (!name.trim()) return;
    setSaving(true);
    try {
      await createCluster(token, { name, provider, region, kubeconfig });
      router.push("/clusters");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "创建失败";
      alert(msg);
    } finally {
      setSaving(false);
    }
  }

  return (
    <AppShell title="接入集群">
      <div style={{ maxWidth: 640, margin: "0 auto", padding: 24 }}>
        <h1 style={{ fontSize: 18, fontWeight: 590, marginBottom: 24 }}>接入新集群</h1>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            集群名称
          </label>
          <input
            type="text"
            className="input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="production-k8s"
          />
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            Provider
          </label>
          <select className="input" value={provider} onChange={(e) => setProvider(e.target.value)}>
            <option value="selfhosted">自建 (kubeconfig)</option>
            <option value="tke" disabled>TKE (M5 开放)</option>
            <option value="ack" disabled>ACK (M5 开放)</option>
            <option value="eks" disabled>EKS (M5 开放)</option>
          </select>
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            区域
          </label>
          <input
            type="text"
            className="input"
            value={region}
            onChange={(e) => setRegion(e.target.value)}
            placeholder="cn-hangzhou"
          />
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            Kubeconfig
          </label>
          <textarea
            className="input"
            rows={10}
            value={kubeconfig}
            onChange={(e) => setKubeconfig(e.target.value)}
            placeholder={`apiVersion: v1
kind: Config
clusters:
  - cluster:
      server: https://...
    name: default
contexts:
  - context:
      cluster: default
      user: default
    name: default
current-context: default
users:
  - name: default
    user:
      token: ...`}
            style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}
          />
        </div>

        {testResult && (
          <div
            style={{
              padding: "8px 12px",
              borderRadius: 6,
              marginBottom: 16,
              fontSize: 12,
              background: testResult.startsWith("连接成功")
                ? "rgba(52,211,153,0.1)"
                : "rgba(248,113,113,0.1)",
              color: testResult.startsWith("连接成功") ? "var(--success)" : "var(--danger)",
              border: `1px solid ${testResult.startsWith("连接成功") ? "rgba(52,211,153,0.25)" : "rgba(248,113,113,0.25)"}`,
            }}
          >
            {testResult}
          </div>
        )}

        <div style={{ display: "flex", gap: 12 }}>
          <button className="btn bsm" onClick={handleTest} disabled={testing || !kubeconfig.trim()}>
            {testing ? "测试中…" : "连通性测试"}
          </button>
          <button className="btn btn-primary" onClick={handleSubmit} disabled={saving || !name.trim()}>
            {saving ? "保存中…" : "保存"}
          </button>
        </div>
      </div>
    </AppShell>
  );
}
