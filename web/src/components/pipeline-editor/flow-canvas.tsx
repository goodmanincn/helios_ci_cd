"use client";

import { useCallback } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  addEdge,
  type Connection,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

const initialNodes: Node[] = [
  {
    id: "checkout",
    position: { x: 250, y: 20 },
    data: { label: "拉取代码", icon: "📥", sub: "git · alpine" },
    type: "step",
  },
  {
    id: "test",
    position: { x: 250, y: 150 },
    data: { label: "单元测试", icon: "🧪", sub: "go test" },
    type: "step",
  },
  {
    id: "build",
    position: { x: 250, y: 280 },
    data: { label: "构建镜像", icon: "📦", sub: "kaniko" },
    type: "step",
  },
];

const initialEdges: Edge[] = [
  { id: "e-checkout-test", source: "checkout", target: "test", type: "custom" },
  { id: "e-test-build", source: "test", target: "build", type: "custom" },
];

function StepNode({ data }: { data: Record<string, unknown> }) {
  return (
    <div
      style={{
        background: "var(--bg-elev)",
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: "10px 12px",
        display: "flex",
        alignItems: "center",
        gap: 10,
        cursor: "move",
        boxShadow: "0 2px 4px rgba(0,0,0,0.4)",
        minWidth: 180,
      }}
    >
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
        {String(data.icon ?? "⚙")}
      </div>
      <div>
        <div style={{ fontSize: 13, fontWeight: 590, color: "var(--fg)" }}>
          {String(data.label ?? "")}
        </div>
        {data.sub != null && (
          <div
            style={{
              fontSize: 10,
              color: "var(--fg-dim)",
              fontFamily: "var(--font-mono), monospace",
              marginTop: 1,
            }}
          >
            {String(data.sub)}
          </div>
        )}
      </div>
    </div>
  );
}

const nodeTypes = { step: StepNode };

export default function FlowCanvas() {
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  const onConnect = useCallback(
    (params: Connection) => setEdges((eds) => addEdge(params, eds)),
    [setEdges],
  );

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onConnect={onConnect}
      nodeTypes={nodeTypes}
      fitView
      style={{ background: "var(--bg)" }}
    >
      <Background
        gap={24}
        size={1}
        color="rgba(255,255,255,0.04)"
        style={{ background: "radial-gradient(circle, rgba(255,255,255,0.04) 1px, transparent 1px) 0 0 / 24px 24px, var(--bg)" }}
      />
      <Controls />
      <MiniMap
        nodeStrokeWidth={3}
        style={{ background: "var(--bg-elev)", border: "1px solid var(--border)" }}
      />
    </ReactFlow>
  );
}
