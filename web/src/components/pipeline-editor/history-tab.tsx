"use client";

import { useEffect, useState } from "react";
import { useAuthStore } from "@/lib/auth-store";
import {
  getPipelineVersions,
  restorePipelineVersion,
  type PipelineVersion,
} from "@/lib/pipelines-api";

export default function HistoryTab({ pipelineId }: { pipelineId: string }) {
  const token = useAuthStore((s) => s.accessToken);
  const [versions, setVersions] = useState<PipelineVersion[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function load() {
    setLoading(true);
    try {
      const list = await getPipelineVersions(token, pipelineId);
      setVersions(list);
      setError(null);
    } catch (e) {
      setError("加载版本历史失败");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, [pipelineId, token]);

  async function onRestore(v: number) {
    if (!window.confirm(`确定回滚到 v${v} ? 这会创建一个新版本。`)) return;
    try {
      await restorePipelineVersion(token, pipelineId, v);
      load();
    } catch {
      alert("回滚失败");
    }
  }

  if (loading) {
    return (
      <div style={{ padding: 24, color: "var(--fg-dim)" }}>加载中…</div>
    );
  }
  if (error) {
    return (
      <div style={{ padding: 24, color: "var(--danger)" }}>{error}</div>
    );
  }

  return (
    <div style={{ flex: 1, overflow: "auto", padding: 24 }}>
      <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
        <thead>
          <tr style={{ borderBottom: "1px solid var(--border)" }}>
            <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>版本</th>
            <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>备注</th>
            <th style={{ textAlign: "left", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>时间</th>
            <th style={{ textAlign: "right", padding: "8px 12px", color: "var(--fg-dim)", fontWeight: 510 }}>操作</th>
          </tr>
        </thead>
        <tbody>
          {versions.map((v) => (
            <tr key={v.id} style={{ borderBottom: "1px solid var(--border)" }}>
              <td style={{ padding: "10px 12px", color: "var(--fg)" }}>
                v{v.version}
              </td>
              <td style={{ padding: "10px 12px", color: "var(--fg-mute)" }}>
                {v.message || "—"}
              </td>
              <td style={{ padding: "10px 12px", color: "var(--fg-dim)", fontSize: 12 }}>
                {new Date(v.created_at).toLocaleString()}
              </td>
              <td style={{ padding: "10px 12px", textAlign: "right" }}>
                <button className="btn bsm" onClick={() => onRestore(v.version)}>
                  回滚
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {versions.length === 0 && (
        <div style={{ padding: 24, color: "var(--fg-dim)", textAlign: "center" }}>
          暂无版本历史
        </div>
      )}
    </div>
  );
}
