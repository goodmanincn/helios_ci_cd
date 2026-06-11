"use client";

import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";

export interface NotifyNodeData {
  label: string;
  sub?: string;
  [key: string]: unknown;
}

type NotifyNode = Node<NotifyNodeData, "notify">;

export default function NotifyNode({ selected, data }: NodeProps<NotifyNode>) {
  return (
    <div
      style={{
        background: "rgba(52,211,153,0.06)",
        border: `1px solid ${selected ? "#34d399" : "rgba(52,211,153,0.25)"}`,
        borderRadius: 8,
        padding: "10px 12px",
        display: "flex",
        alignItems: "center",
        gap: 10,
        cursor: "move",
        boxShadow: selected
          ? "0 0 0 1px #34d399, 0 2px 4px rgba(0,0,0,0.4)"
          : "0 2px 4px rgba(0,0,0,0.4)",
        minWidth: 180,
        transition: "border-color 0.15s, box-shadow 0.15s",
      }}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{
          width: 8,
          height: 8,
          background: "#34d399",
          border: "2px solid var(--bg-elev)",
        }}
      />
      <div
        style={{
          width: 28,
          height: 28,
          background: "rgba(52,211,153,0.1)",
          borderRadius: 5,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          fontSize: 14,
          flexShrink: 0,
        }}
      >
        📢
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
        {data.sub != null && (
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
            {data.sub}
          </div>
        )}
      </div>
      <Handle
        type="source"
        position={Position.Right}
        style={{
          width: 8,
          height: 8,
          background: "#34d399",
          border: "2px solid var(--bg-elev)",
        }}
      />
    </div>
  );
}
