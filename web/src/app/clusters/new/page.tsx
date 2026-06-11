"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import {
  AckCloudCreds,
  createCluster,
  discoverClusters,
  TkeCloudCreds,
  testCluster,
} from "@/lib/clusters-api";

type Provider = "selfhosted" | "tke" | "ack";

const TKE_REGIONS = [
  "ap-guangzhou",
  "ap-shanghai",
  "ap-beijing",
  "ap-nanjing",
  "ap-chengdu",
  "ap-chongqing",
  "ap-hongkong",
];

const ACK_REGIONS = [
  "cn-hangzhou",
  "cn-shanghai",
  "cn-beijing",
  "cn-shenzhen",
  "cn-hongkong",
  "cn-guangzhou",
];

export default function NewClusterPage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [step, setStep] = useState(1);
  const [provider, setProvider] = useState<Provider>("selfhosted");
  const [name, setName] = useState("");
  const [region, setRegion] = useState("");
  const [kubeconfig, setKubeconfig] = useState("");

  const [secretId, setSecretId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [accessKeyId, setAccessKeyId] = useState("");
  const [accessKeySecret, setAccessKeySecret] = useState("");
  const [roleArn, setRoleArn] = useState("");
  const [clusterId, setClusterId] = useState("");
  const [remoteClusters, setRemoteClusters] = useState<{ id: string; name: string; version?: string }[]>([]);

  const [discovering, setDiscovering] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  function cloudRegion() {
    if (region) return region;
    return provider === "tke" ? TKE_REGIONS[0] : ACK_REGIONS[0];
  }

  function buildTkeCloud(): TkeCloudCreds {
    return {
      secret_id: secretId,
      secret_key: secretKey,
      region: cloudRegion(),
      role_arn: roleArn || undefined,
      cluster_id: clusterId || undefined,
    };
  }

  function buildAckCloud(): AckCloudCreds {
    return {
      access_key_id: accessKeyId,
      access_key_secret: accessKeySecret,
      region: cloudRegion(),
      role_arn: roleArn || undefined,
      cluster_id: clusterId || undefined,
    };
  }

  async function handleDiscover() {
    setDiscovering(true);
    setTestResult(null);
    try {
      if (provider === "tke") {
        const res = await discoverClusters(token, "tke", buildTkeCloud());
        setRemoteClusters(res.clusters);
        if (res.clusters.length > 0 && !clusterId) {
          setClusterId(res.clusters[0].id);
        }
      } else if (provider === "ack") {
        const res = await discoverClusters(token, "ack", buildAckCloud());
        setRemoteClusters(res.clusters);
        if (res.clusters.length > 0 && !clusterId) {
          setClusterId(res.clusters[0].id);
        }
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : "拉取集群列表失败";
      setTestResult(`失败: ${msg}`);
    } finally {
      setDiscovering(false);
    }
  }

  async function handleTest() {
    setTesting(true);
    setTestResult(null);
    try {
      let info;
      if (provider === "selfhosted") {
        info = await testCluster(token, { provider, kubeconfig });
      } else if (provider === "tke") {
        info = await testCluster(token, { provider: "tke", cloud: buildTkeCloud() });
      } else {
        info = await testCluster(token, { provider: "ack", cloud: buildAckCloud() });
      }
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
      const reg = provider === "selfhosted" ? region : cloudRegion();
      if (provider === "selfhosted") {
        await createCluster(token, { name, provider, region: reg, kubeconfig });
      } else if (provider === "tke") {
        await createCluster(token, { name, provider: "tke", region: reg, cloud: buildTkeCloud() });
      } else {
        await createCluster(token, { name, provider: "ack", region: reg, cloud: buildAckCloud() });
      }
      router.push("/clusters");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "创建失败";
      alert(msg);
    } finally {
      setSaving(false);
    }
  }

  const canTest =
    provider === "selfhosted"
      ? kubeconfig.trim().length > 0
      : provider === "tke"
        ? secretId && secretKey && clusterId
        : accessKeyId && accessKeySecret && clusterId;

  return (
    <AppShell title="接入集群">
      <div style={{ maxWidth: 720, margin: "0 auto", padding: 24 }}>
        <h1 style={{ fontSize: 18, fontWeight: 590, marginBottom: 8 }}>接入新集群</h1>
        <p style={{ fontSize: 13, color: "var(--fg-dim)", marginBottom: 24 }}>
          步骤 {step}/3 · {step === 1 ? "选择云厂商" : step === 2 ? "配置凭据" : "测试并保存"}
        </p>

        {step === 1 && (
          <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 12 }}>
            {(
              [
                { id: "selfhosted" as const, label: "自建 K8s", sub: "kubeconfig", color: "#5e6ad2" },
                { id: "tke" as const, label: "腾讯 TKE", sub: "SecretId / Key", color: "#006dde" },
                { id: "ack" as const, label: "阿里 ACK", sub: "AccessKey", color: "#ff6b00" },
              ] as const
            ).map((card) => (
              <button
                key={card.id}
                type="button"
                onClick={() => {
                  setProvider(card.id);
                  setStep(2);
                  if (card.id === "tke" && !region) setRegion(TKE_REGIONS[0]);
                  if (card.id === "ack" && !region) setRegion(ACK_REGIONS[0]);
                }}
                style={{
                  textAlign: "left",
                  padding: 16,
                  borderRadius: 10,
                  border: `1px solid ${provider === card.id ? card.color : "var(--border)"}`,
                  background: provider === card.id ? `${card.color}14` : "var(--bg-elev)",
                  cursor: "pointer",
                }}
              >
                <div style={{ fontWeight: 590, marginBottom: 4 }}>{card.label}</div>
                <div style={{ fontSize: 12, color: "var(--fg-dim)" }}>{card.sub}</div>
              </button>
            ))}
          </div>
        )}

        {step === 2 && (
          <div>
            <button type="button" className="btn bsm" style={{ marginBottom: 16 }} onClick={() => setStep(1)}>
              ← 换 Provider
            </button>

            <div style={{ marginBottom: 16 }}>
              <label style={labelStyle}>集群名称</label>
              <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="prod-tke-shanghai" />
            </div>

            {provider === "selfhosted" && (
              <>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>区域 (可选)</label>
                  <input className="input" value={region} onChange={(e) => setRegion(e.target.value)} placeholder="local" />
                </div>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>Kubeconfig</label>
                  <textarea
                    className="input"
                    rows={10}
                    value={kubeconfig}
                    onChange={(e) => setKubeconfig(e.target.value)}
                    style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}
                  />
                </div>
              </>
            )}

            {provider === "tke" && (
              <>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>区域</label>
                  <select className="input" value={region || TKE_REGIONS[0]} onChange={(e) => setRegion(e.target.value)}>
                    {TKE_REGIONS.map((r) => (
                      <option key={r} value={r}>
                        {r}
                      </option>
                    ))}
                  </select>
                </div>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>SecretId</label>
                  <input className="input" value={secretId} onChange={(e) => setSecretId(e.target.value)} />
                </div>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>SecretKey</label>
                  <input className="input" type="password" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} />
                </div>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>Role ARN (可选, STS)</label>
                  <input className="input" value={roleArn} onChange={(e) => setRoleArn(e.target.value)} placeholder="qcs::cam::uin/...:roleName/..." />
                </div>
                <div style={{ display: "flex", gap: 8, marginBottom: 16 }}>
                  <button type="button" className="btn bsm" onClick={handleDiscover} disabled={discovering || !secretId || !secretKey}>
                    {discovering ? "拉取中…" : "拉取集群列表"}
                  </button>
                </div>
                {remoteClusters.length > 0 && (
                  <div style={{ marginBottom: 16 }}>
                    <label style={labelStyle}>集群</label>
                    <select className="input" value={clusterId} onChange={(e) => setClusterId(e.target.value)}>
                      {remoteClusters.map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.name || c.id} {c.version ? `· k8s ${c.version}` : ""}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
              </>
            )}

            {provider === "ack" && (
              <>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>区域</label>
                  <select className="input" value={region || ACK_REGIONS[0]} onChange={(e) => setRegion(e.target.value)}>
                    {ACK_REGIONS.map((r) => (
                      <option key={r} value={r}>
                        {r}
                      </option>
                    ))}
                  </select>
                </div>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>AccessKey ID</label>
                  <input className="input" value={accessKeyId} onChange={(e) => setAccessKeyId(e.target.value)} />
                </div>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>AccessKey Secret</label>
                  <input className="input" type="password" value={accessKeySecret} onChange={(e) => setAccessKeySecret(e.target.value)} />
                </div>
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>Role ARN (可选, STS)</label>
                  <input className="input" value={roleArn} onChange={(e) => setRoleArn(e.target.value)} />
                </div>
                <div style={{ display: "flex", gap: 8, marginBottom: 16 }}>
                  <button type="button" className="btn bsm" onClick={handleDiscover} disabled={discovering || !accessKeyId || !accessKeySecret}>
                    {discovering ? "拉取中…" : "拉取集群列表"}
                  </button>
                </div>
                {remoteClusters.length > 0 && (
                  <div style={{ marginBottom: 16 }}>
                    <label style={labelStyle}>集群</label>
                    <select className="input" value={clusterId} onChange={(e) => setClusterId(e.target.value)}>
                      {remoteClusters.map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.name || c.id} {c.version ? `· k8s ${c.version}` : ""}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
              </>
            )}

            <button type="button" className="btn btn-primary" onClick={() => setStep(3)} disabled={!name.trim()}>
              下一步 →
            </button>
          </div>
        )}

        {step === 3 && (
          <div>
            <button type="button" className="btn bsm" style={{ marginBottom: 16 }} onClick={() => setStep(2)}>
              ← 修改配置
            </button>
            <div style={{ fontSize: 13, marginBottom: 16, color: "var(--fg-dim)" }}>
              {name} · {provider.toUpperCase()} · {provider === "selfhosted" ? region || "—" : cloudRegion()}
              {clusterId ? ` · ${clusterId}` : ""}
            </div>

            {testResult && (
              <div
                style={{
                  padding: "8px 12px",
                  borderRadius: 6,
                  marginBottom: 16,
                  fontSize: 12,
                  background: testResult.startsWith("连接成功") ? "rgba(52,211,153,0.1)" : "rgba(248,113,113,0.1)",
                  color: testResult.startsWith("连接成功") ? "var(--success)" : "var(--danger)",
                }}
              >
                {testResult}
              </div>
            )}

            <div style={{ display: "flex", gap: 12 }}>
              <button className="btn bsm" onClick={handleTest} disabled={testing || !canTest}>
                {testing ? "测试中…" : "连通性测试"}
              </button>
              <button className="btn btn-primary" onClick={handleSubmit} disabled={saving || !name.trim()}>
                {saving ? "保存中…" : "保存"}
              </button>
            </div>
          </div>
        )}
      </div>
    </AppShell>
  );
}

const labelStyle: React.CSSProperties = {
  fontSize: 12,
  fontWeight: 510,
  color: "var(--fg-dim)",
  display: "block",
  marginBottom: 6,
};
