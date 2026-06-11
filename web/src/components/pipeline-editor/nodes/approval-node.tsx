"use client";

import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import NodeErrorBadge from "./node-error-badge";

export interface ApprovalNodeData {
  label: string;
  approvers?: string;
  [key: string]: unknown;
}

type ApprovalNode = Node<ApprovalNodeData, "approval">;

export default function ApprovalNode({ id, selected, data }: NodeProps<ApprovalNode>) {
  return (
    <div
      style={{
        background: "rgba(245,158,11,0.08)",
        border: `1px solid ${selected ? "#f59e0b" : "rgba(245,158,11,0.3)"}`,
        borderRadius: 8,
        padding: "10px 12px",
        display: "flex",
        alignItems: "center",
        gap: 10,
        cursor: "move",
        boxShadow: selected
          ? "0 0 0 1px #f59e0b, 0 2px 4px rgba(0,0,0,0.4)"
          : "0 2px 4px rgba(0,0,0,0.4)",
        minWidth: 180,
        transition: "border-color 0.15s, box-shadow 0.15s",
        position: "relative",
      }}
    >
      <NodeErrorBadge nodeId={id} />
      <Handle
        type="target"
        position={Position.Left}
        style={{
          width: 8,
          height: 8,
          background: "#f59e0b",
          border: "2px solid var(--bg-elev)",
        }}
      />
      <div
        style={{
          width: 28,
          height: 28,
          background: "rgba(245,158,11,0.12)",
          borderRadius: 5,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          fontSize: 14,
          flexShrink: 0,
        }}
      >
        ✋
      </div>
      <div style={{ minWidth: 0 }}>
        <div
          style={{
            fontSize: 13,
            fontWeight: 590,
            color: "var(--fg)",
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
        >
          {data.label}
        </div>
        {data.approvers != null && (
          <div
            style={{
              fontSize: 10,
              color: "var(--fg-dim)",
              fontFamily: "var(--font-mono), monospace",
              marginTop: 1,
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {data.approvers}
          </div>
        )}
      </div>
      <Handle
        type="source"
        position={Position.Right}
        style={{
          width: 8,
          height: 8,
          background: "#f59e0b",
          border: "2px solid var(--bg-elev)",
        }}
      />
    </div>
  );
}
