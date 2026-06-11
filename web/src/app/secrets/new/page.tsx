"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import { createSecret, type SecretType } from "@/lib/secrets-api";

const TYPES: { value: SecretType; label: string }[] = [
  { value: "text", label: "纯文本" },
  { value: "file", label: "文件内容" },
  { value: "kubeconfig", label: "Kubeconfig" },
  { value: "ssh-key", label: "SSH 私钥" },
  { value: "tencent_cloud", label: "腾讯云 (TKE)" },
  { value: "aliyun_cloud", label: "阿里云 (ACK)" },
  { value: "cloud-credential", label: "通用云凭据" },
];

const TKE_REGIONS = [
  "ap-guangzhou", "ap-shanghai", "ap-beijing", "ap-nanjing", "ap-chengdu", "ap-chongqing", "ap-hongkong",
];

const ACK_REGIONS = [
  "cn-hangzhou", "cn-shanghai", "cn-beijing", "cn-shenzhen", "cn-hongkong", "cn-guangzhou",
];

export default function NewSecretPage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const orgId = useAuthStore((s) => s.orgs[0]);
  const [name, setName] = useState("");
  const [typ, setTyp] = useState<SecretType>("text");
  const [description, setDescription] = useState("");
  const [plainValue, setPlainValue] = useState("");
  const [secretId, setSecretId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [accessKeyId, setAccessKeyId] = useState("");
  const [accessKeySecret, setAccessKeySecret] = useState("");
  const [region, setRegion] = useState("");
  const [roleArn, setRoleArn] = useState("");
  const [saving, setSaving] = useState(false);

  function buildValue(): string {
    if (typ === "tencent_cloud") {
      return JSON.stringify({
        secret_id: secretId.trim(),
        secret_key: secretKey.trim(),
        region: region || TKE_REGIONS[0],
        ...(roleArn.trim() ? { role_arn: roleArn.trim() } : {}),
      });
    }
    if (typ === "aliyun_cloud") {
      return JSON.stringify({
        access_key_id: accessKeyId.trim(),
        access_key_secret: accessKeySecret.trim(),
        region: region || ACK_REGIONS[0],
        ...(roleArn.trim() ? { role_arn: roleArn.trim() } : {}),
      });
    }
    return plainValue;
  }

  function canSubmit(): boolean {
    if (!name.trim() || orgId == null) return false;
    if (typ === "tencent_cloud") {
      return secretId.trim() !== "" && secretKey.trim() !== "";
    }
    if (typ === "aliyun_cloud") {
      return accessKeyId.trim() !== "" && accessKeySecret.trim() !== "";
    }
    return plainValue.trim() !== "";
  }

  async function handleSubmit() {
    if (!canSubmit() || orgId == null) return;
    setSaving(true);
    try {
      await createSecret(token, {
        scope: "org",
        scope_id: orgId,
        name: name.trim(),
        type: typ,
        description: description.trim() || undefined,
        value: buildValue(),
      });
      router.push("/secrets");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "创建失败";
      alert(msg);
    } finally {
      setSaving(false);
    }
  }

  return (
    <AppShell title="新建密钥">
      <div style={{ maxWidth: 640, margin: "0 auto", padding: 24 }}>
        <h1 style={{ fontSize: 18, fontWeight: 590, marginBottom: 24 }}>新建密钥</h1>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            名称
          </label>
          <input
            type="text"
            className="input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="TKE_PROD_CREDS"
          />
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            类型
          </label>
          <select
            className="input"
            value={typ}
            onChange={(e) => setTyp(e.target.value as SecretType)}
          >
            {TYPES.map((t) => (
              <option key={t.value} value={t.value}>{t.label}</option>
            ))}
          </select>
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
            描述 (可选)
          </label>
          <input
            type="text"
            className="input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="生产环境 TKE 主账号凭据"
          />
        </div>

        {typ === "tencent_cloud" && (
          <>
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
                SecretId
              </label>
              <input type="text" className="input" value={secretId} onChange={(e) => setSecretId(e.target.value)} />
            </div>
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
                SecretKey
              </label>
              <input type="password" className="input" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} />
            </div>
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
                区域
              </label>
              <select className="input" value={region || TKE_REGIONS[0]} onChange={(e) => setRegion(e.target.value)}>
                {TKE_REGIONS.map((r) => (
                  <option key={r} value={r}>{r}</option>
                ))}
              </select>
            </div>
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
                Role ARN (可选, STS 扮演)
              </label>
              <input type="text" className="input" value={roleArn} onChange={(e) => setRoleArn(e.target.value)} placeholder="qcs::cam::uin/xxx:roleName/xxx" />
            </div>
          </>
        )}

        {typ === "aliyun_cloud" && (
          <>
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
                AccessKey ID
              </label>
              <input type="text" className="input" value={accessKeyId} onChange={(e) => setAccessKeyId(e.target.value)} />
            </div>
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
                AccessKey Secret
              </label>
              <input type="password" className="input" value={accessKeySecret} onChange={(e) => setAccessKeySecret(e.target.value)} />
            </div>
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
                区域
              </label>
              <select className="input" value={region || ACK_REGIONS[0]} onChange={(e) => setRegion(e.target.value)}>
                {ACK_REGIONS.map((r) => (
                  <option key={r} value={r}>{r}</option>
                ))}
              </select>
            </div>
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
                Role ARN (可选, STS 扮演)
              </label>
              <input type="text" className="input" value={roleArn} onChange={(e) => setRoleArn(e.target.value)} placeholder="acs:ram::xxx:role/xxx" />
            </div>
          </>
        )}

        {typ !== "tencent_cloud" && typ !== "aliyun_cloud" && (
          <div style={{ marginBottom: 24 }}>
            <label style={{ fontSize: 12, fontWeight: 510, color: "var(--fg-dim)", display: "block", marginBottom: 6 }}>
              密钥内容
            </label>
            <textarea
              className="input"
              rows={8}
              value={plainValue}
              onChange={(e) => setPlainValue(e.target.value)}
              placeholder={typ === "kubeconfig" ? "粘贴 kubeconfig YAML" : "输入密钥内容"}
              style={{ width: "100%", fontFamily: "var(--font-mono)", fontSize: 12 }}
            />
          </div>
        )}

        <div style={{ display: "flex", gap: 12 }}>
          <button className="btn" onClick={() => router.push("/secrets")}>取消</button>
          <button className="btn btn-primary" onClick={handleSubmit} disabled={saving || !canSubmit()}>
            {saving ? "保存中…" : "保存"}
          </button>
        </div>
      </div>
    </AppShell>
  );
}
