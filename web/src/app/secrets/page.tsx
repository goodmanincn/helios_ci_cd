"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useAuthStore } from "@/lib/auth-store";
import {
  deleteSecret,
  listSecrets,
  updateSecret,
  type Secret,
  type SecretType,
} from "@/lib/secrets-api";

const TYPE_LABEL: Record<SecretType, string> = {
  text: "纯文本",
  file: "文件",
  kubeconfig: "Kubeconfig",
  "ssh-key": "SSH 私钥",
  "cloud-credential": "云凭据",
  tencent_cloud: "腾讯云",
  aliyun_cloud: "阿里云",
};

export default function SecretsPage() {
  const router = useRouter();
  const token = useAuthStore((s) => s.accessToken);
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [loading, setLoading] = useState(true);
  const [q, setQ] = useState("");
  const [rotateId, setRotateId] = useState<number | null>(null);
  const [rotateValue, setRotateValue] = useState("");
  const [rotating, setRotating] = useState(false);

  const reload = useCallback(() => {
    setLoading(true);
    listSecrets(token, { q: q || undefined })
      .then((r) => setSecrets(r.items))
      .catch(() => setSecrets([]))
      .finally(() => setLoading(false));
  }, [token, q]);

  useEffect(() => {
    reload();
  }, [reload]);

  async function handleDelete(s: Secret) {
    if (!confirm(`确定删除密钥「${s.name}」？`)) return;
    try {
      await deleteSecret(token, s.id);
      reload();
    } catch (e) {
      const msg = e instanceof Error ? e.message : "删除失败";
      alert(msg);
    }
  }

  async function handleRotate() {
    if (!rotateId || !rotateValue.trim()) return;
    setRotating(true);
    try {
      await updateSecret(token, rotateId, { value: rotateValue });
      setRotateId(null);
      setRotateValue("");
      reload();
    } catch (e) {
      const msg = e instanceof Error ? e.message : "轮换失败";
      alert(msg);
    } finally {
      setRotating(false);
    }
  }

  const rotatingSecret = secrets.find((s) => s.id === rotateId);

  return (
    <AppShell title="密钥">
      <div style={{ padding: 24 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <h1 style={{ fontSize: 18, fontWeight: 590, margin: 0 }}>密钥保险箱</h1>
            <input
              type="text"
              className="input"
              placeholder="搜索名称或描述"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              style={{ width: 220, fontSize: 12 }}
            />
          </div>
          <button className="btn btn-primary" onClick={() => router.push("/secrets/new")}>
            + 新建密钥
          </button>
        </div>

        {loading && <div style={{ color: "var(--fg-dim)" }}>加载中…</div>}

        <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: "1px solid var(--border)" }}>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>名称</th>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>类型</th>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>关联资源</th>
              <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>更新时间</th>
              <th style={{ textAlign: "right", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {secrets.map((s) => (
              <tr key={s.id} style={{ borderBottom: "1px solid var(--border)" }}>
                <td style={{ padding: "10px 12px" }}>
                  <div style={{ fontWeight: 510, color: "var(--fg)" }}>{s.name}</div>
                  {s.description && (
                    <div style={{ fontSize: 11, color: "var(--fg-dim)", marginTop: 2 }}>{s.description}</div>
                  )}
                </td>
                <td style={{ padding: "10px 12px", color: "var(--fg-mute)" }}>
                  {TYPE_LABEL[s.type] || s.type}
                </td>
                <td style={{ padding: "10px 12px" }}>
                  {s.references && s.references.length > 0 ? (
                    <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                      {s.references.map((r) => (
                        <span
                          key={`${r.kind}-${r.id}`}
                          style={{
                            fontSize: 10,
                            padding: "1px 6px",
                            borderRadius: 3,
                            background: "rgba(255,255,255,0.05)",
                            color: "var(--fg-dim)",
                          }}
                        >
                          {r.kind === "cluster" ? "☸" : "🖥"} {r.name}
                        </span>
                      ))}
                    </div>
                  ) : (
                    <span style={{ color: "var(--fg-dim)", fontSize: 12 }}>—</span>
                  )}
                </td>
                <td style={{ padding: "10px 12px", color: "var(--fg-dim)", fontSize: 12 }}>
                  {new Date(s.updated_at).toLocaleString()}
                </td>
                <td style={{ padding: "10px 12px", textAlign: "right" }}>
                  <div style={{ display: "flex", gap: 6, justifyContent: "flex-end" }}>
                    <button
                      className="btn bsm"
                      style={{ padding: "2px 8px", fontSize: 11 }}
                      onClick={() => {
                        setRotateId(s.id);
                        setRotateValue("");
                      }}
                    >
                      轮换
                    </button>
                    <button
                      className="btn bsm"
                      style={{ padding: "2px 8px", fontSize: 11, color: "var(--danger)" }}
                      onClick={() => handleDelete(s)}
                      disabled={(s.references?.length ?? 0) > 0}
                      title={(s.references?.length ?? 0) > 0 ? "被引用的密钥不能删除" : undefined}
                    >
                      删除
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {!loading && secrets.length === 0 && (
          <div style={{ color: "var(--fg-dim)", textAlign: "center", padding: 48 }}>
            暂无密钥，点击右上角「新建密钥」开始
          </div>
        )}
      </div>

      {rotateId != null && (
        <div
          style={{
            position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)",
            display: "flex", alignItems: "center", justifyContent: "center", zIndex: 100,
          }}
          onClick={() => setRotateId(null)}
        >
          <div
            className="card"
            style={{ width: 480, maxWidth: "90vw", padding: 20 }}
            onClick={(e) => e.stopPropagation()}
          >
            <h2 style={{ fontSize: 15, fontWeight: 590, margin: "0 0 12px" }}>
              轮换密钥 · {rotatingSecret?.name}
            </h2>
            <p style={{ fontSize: 12, color: "var(--fg-dim)", marginBottom: 12 }}>
              输入新值后将重新加密存储。旧值不会显示。
            </p>
            <textarea
              className="input"
              rows={6}
              value={rotateValue}
              onChange={(e) => setRotateValue(e.target.value)}
              placeholder="新密钥内容 (云凭据请粘贴完整 JSON)"
              style={{ width: "100%", fontFamily: "var(--font-mono)", fontSize: 12, marginBottom: 16 }}
            />
            <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
              <button className="btn" onClick={() => setRotateId(null)}>取消</button>
              <button
                className="btn btn-primary"
                onClick={handleRotate}
                disabled={rotating || !rotateValue.trim()}
              >
                {rotating ? "保存中…" : "确认轮换"}
              </button>
            </div>
          </div>
        </div>
      )}
    </AppShell>
  );
}
