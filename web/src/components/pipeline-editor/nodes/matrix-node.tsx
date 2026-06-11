"use client";

import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";

export interface MatrixNodeData {
  label: string;
  [key: string]: unknown;
}

type MatrixNode = Node<MatrixNodeData, "matrix">;

export default function MatrixNode({ selected, data }: NodeProps<MatrixNode>) {
  return (
    <div
      style={{
        background: "rgba(94,106,210,0.08)",
        border: `1px solid ${selected ? "#8b5cf6" : "rgba(94,106,210,0.3)"}`,
        borderRadius: 8,
        padding: "8px 12px",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        cursor: "move",
        boxShadow: selected
          ? "0 0 0 1px #8b5cf6, 0 2px 4px rgba(0,0,0,0.4)"
          : "0 2px 4px rgba(0,0,0,0.4)",
        minWidth: 90,
        transition: "border-color 0.15s, box-shadow 0.15s",
      }}
    >
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
          fontSize: 11,
          fontWeight: 590,
          color: "var(--fg)",
          whiteSpace: "nowrap",
        }}
      >
        {data.label}
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
