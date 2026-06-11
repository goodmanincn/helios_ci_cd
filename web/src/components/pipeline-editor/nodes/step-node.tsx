"use client";

import { useState } from "react";
import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import NodeErrorBadge from "./node-error-badge";

export interface StepNodeData {
  label: string;
  icon?: string;
  sub?: string;
  [key: string]: unknown;
}

type StepNode = Node<StepNodeData, "step">;

export default function StepNode({ id, selected, data }: NodeProps<StepNode>) {
  const [hover, setHover] = useState(false);
  const icon = data.icon ?? "⚙";

  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        background: "var(--bg-elev)",
        border: `1px solid ${selected ? "#8b5cf6" : hover ? "var(--border-strong)" : "var(--border)"}`,
        borderRadius: 8,
        padding: "10px 12px",
        display: "flex",
        alignItems: "center",
        gap: 10,
        cursor: "move",
        boxShadow: selected
          ? "0 0 0 1px #8b5cf6, 0 2px 4px rgba(0,0,0,0.4)"
          : "0 2px 4px rgba(0,0,0,0.4)",
        minWidth: 180,
        position: "relative",
        transition: "border-color 0.15s, box-shadow 0.15s",
      }}
    >
      <NodeErrorBadge nodeId={id} />
      <Handle
        type="target"
        position={Position.Left}
        style={{
          width: 8,
          height: 8,
          background: "#8b5cf6",
          border: "2px solid var(--bg-elev)",
        }}
      />

      <div
        style={{
          width: 28,
          height: 28,
          background: "rgba(255,255,255,0.05)",
          borderRadius: 5,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          fontSize: 14,
          flexShrink: 0,
        }}
      >
        {icon}
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
          background: "#8b5cf6",
          border: "2px solid var(--bg-elev)",
        }}
      />
    </div>
  );
}
