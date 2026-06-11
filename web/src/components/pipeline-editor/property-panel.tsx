"use client";

import { useEditorStore } from "./editor-store";

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div style={{ marginBottom: 14 }}>
      <label
        style={{
          display: "block",
          fontSize: 11,
          fontWeight: 510,
          color: "var(--fg-dim)",
          textTransform: "uppercase",
          letterSpacing: "0.4px",
          marginBottom: 5,
        }}
      >
        {label}
      </label>
      {children}
    </div>
  );
}

export default function PropertyPanel() {
  const selectedNodeId = useEditorStore((s) => s.selectedNodeId);
  const nodes = useEditorStore((s) => s.nodes);
  const updateNodeData = useEditorStore((s) => s.updateNodeData);

  const node = nodes.find((n) => n.id === selectedNodeId);

  if (!node) {
    return (
      <div
        style={{
          padding: 8,
          borderRadius: 5,
          background: "rgba(255,255,255,0.02)",
          color: "var(--fg-dim)",
          fontSize: 12,
          border: "1px dashed var(--border)",
        }}
      >
        选中节点后显示表单…
      </div>
    );
  }

  const data = node.data as Record<string, unknown>;
  const label = String(data.label ?? "");
  const sub = data.sub != null ? String(data.sub) : "";
  const ifExpr = data.if != null ? String(data.if) : "";
  const timeout = data.timeout != null ? String(data.timeout) : "";
  const approvers = data.approvers != null ? String(data.approvers) : "";

  // with 对象编辑
  const withObj =
    data.with != null && typeof data.with === "object"
      ? (data.with as Record<string, unknown>)
      : null;

  // matrix 对象只读展示
  const matrixObj =
    data.matrix != null && typeof data.matrix === "object"
      ? (data.matrix as Record<string, unknown>)
      : null;

  const nodeId = node.id;
  function updateWith(key: string, value: string) {
    const next = { ...(withObj ?? {}), [key]: value };
    updateNodeData(nodeId, { with: next });
  }

  return (
    <div>
      <div
        style={{
          fontSize: 11,
          fontWeight: 510,
          color: "var(--fg-dim)",
          textTransform: "uppercase",
          letterSpacing: "0.4px",
          padding: "0 0 10px",
        }}
      >
        已选中节点
      </div>

      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 18 }}>
        <div
          style={{
            width: 32,
            height: 32,
            background: "rgba(255,255,255,0.05)",
            borderRadius: 5,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: 16,
            flexShrink: 0,
          }}
        >
          {String(data.icon ?? "⚙")}
        </div>
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 590, color: "var(--fg)" }}>
            {label}
          </div>
          <div
            style={{
              fontSize: 11,
              color: "var(--fg-dim)",
              fontFamily: "var(--font-mono), monospace",
            }}
          >
            stage_id: {node.id}
          </div>
        </div>
      </div>

      <Field label="名称">
        <input
          type="text"
          className="input"
          style={{ fontSize: 12, padding: "6px 10px" }}
          value={label}
          onChange={(e) => updateNodeData(node.id, { label: e.target.value })}
        />
      </Field>

      {node.type === "approval" && (
        <>
          <Field label="审批人">
            <input
              type="text"
              className="input"
              style={{ fontSize: 12, padding: "6px 10px" }}
              value={approvers}
              onChange={(e) =>
                updateNodeData(node.id, { approvers: e.target.value })
              }
            />
          </Field>
          <Field label="模式">
            <select
              className="input"
              style={{ fontSize: 12, padding: "6px 10px" }}
              value={String(data.mode ?? "any")}
              onChange={(e) => updateNodeData(node.id, { mode: e.target.value })}
            >
              <option value="any">any (任一通过)</option>
              <option value="all">all (全员通过)</option>
            </select>
          </Field>
        </>
      )}

      {withObj && (
        <>
          <div
            style={{
              fontSize: 11,
              fontWeight: 510,
              color: "var(--fg-dim)",
              textTransform: "uppercase",
              letterSpacing: "0.4px",
              padding: "10px 0 6px",
            }}
          >
            专属配置
          </div>
          {Object.entries(withObj).map(([k, v]) => (
            <Field key={k} label={k}>
              <input
                type="text"
                className="input mono"
                style={{ fontSize: 12, padding: "6px 10px" }}
                value={typeof v === "string" ? v : JSON.stringify(v)}
                onChange={(e) => updateWith(k, e.target.value)}
              />
            </Field>
          ))}
        </>
      )}

      {matrixObj && (
        <Field label="Matrix">
          <pre
            className="mono"
            style={{
              background: "rgba(255,255,255,0.02)",
              border: "1px solid var(--border)",
              borderRadius: 5,
              padding: 8,
              fontSize: 11,
              lineHeight: 1.6,
              color: "var(--fg-dim)",
              whiteSpace: "pre-wrap",
            }}
          >
            {JSON.stringify(matrixObj, null, 2)}
          </pre>
        </Field>
      )}

      <Field label="执行条件 (if)">
        <input
          type="text"
          className="input mono"
          style={{ fontSize: 12, padding: "6px 10px" }}
          value={ifExpr}
          placeholder='branch == "main"'
          onChange={(e) => updateNodeData(node.id, { if: e.target.value })}
        />
      </Field>

      <Field label="超时">
        <input
          type="text"
          className="input mono"
          style={{ fontSize: 12, padding: "6px 10px" }}
          value={timeout}
          placeholder="30m"
          onChange={(e) => updateNodeData(node.id, { timeout: e.target.value })}
        />
      </Field>

      {sub && (
        <Field label="备注">
          <input
            type="text"
            className="input mono"
            style={{ fontSize: 12, padding: "6px 10px" }}
            value={sub}
            onChange={(e) => updateNodeData(node.id, { sub: e.target.value })}
          />
        </Field>
      )}

      <div style={{ display: "flex", gap: 8, marginTop: 20 }}>
        <button className="btn bsm">复制</button>
        <button className="btn bsm btn-ghost" style={{ color: "var(--danger)" }}>
          删除
        </button>
      </div>
    </div>
  );
}
