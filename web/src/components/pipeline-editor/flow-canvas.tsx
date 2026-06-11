"use client";

import { useCallback, useEffect } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  useReactFlow,
  addEdge,
  type Connection,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import StepNode from "./nodes/step-node";
import MatrixNode from "./nodes/matrix-node";
import ApprovalNode from "./nodes/approval-node";
import NotifyNode from "./nodes/notify-node";
import CustomEdge from "./edges/custom-edge";
import { useEditorStore } from "./editor-store";

const nodeTypes = {
  step: StepNode,
  matrix: MatrixNode,
  approval: ApprovalNode,
  notify: NotifyNode,
};

const edgeTypes = {
  custom: CustomEdge,
};

const arrowMarker = {
  type: "arrowclosed" as const,
  color: "#5e6ad2",
};

const initialNodes: Node[] = [
  {
    id: "checkout",
    position: { x: 250, y: 20 },
    data: { label: "拉取代码", icon: "📥", sub: "git · alpine" },
    type: "step",
  },
  {
    id: "test-0",
    position: { x: 80, y: 140 },
    data: { label: "🧪 go 1.21" },
    type: "matrix",
  },
  {
    id: "test-1",
    position: { x: 210, y: 140 },
    data: { label: "🧪 go 1.22" },
    type: "matrix",
  },
  {
    id: "test-2",
    position: { x: 340, y: 140 },
    data: { label: "🧪 go 1.23" },
    type: "matrix",
  },
  {
    id: "build",
    position: { x: 250, y: 260 },
    data: { label: "构建镜像", icon: "📦", sub: "kaniko" },
    type: "step",
  },
  {
    id: "scan",
    position: { x: 250, y: 380 },
    data: { label: "安全扫描", icon: "🛡", sub: "trivy · CRITICAL" },
    type: "step",
  },
  {
    id: "deploy-staging",
    position: { x: 250, y: 500 },
    data: { label: "部署 Staging", icon: "☸", sub: "staging-tke" },
    type: "step",
  },
  {
    id: "approval",
    position: { x: 250, y: 620 },
    data: { label: "人工审批", approvers: "alice, bob" },
    type: "approval",
  },
  {
    id: "deploy-prod",
    position: { x: 250, y: 740 },
    data: { label: "部署 生产", icon: "🚀", sub: "aliyun-ack · Canary" },
    type: "step",
  },
  {
    id: "notify",
    position: { x: 250, y: 860 },
    data: { label: "通知团队", icon: "📢", sub: "钉钉" },
    type: "notify",
  },
];

const initialEdges: Edge[] = [
  { id: "e-c-t0", source: "checkout", target: "test-0", type: "custom", markerEnd: arrowMarker },
  { id: "e-c-t1", source: "checkout", target: "test-1", type: "custom", markerEnd: arrowMarker },
  { id: "e-c-t2", source: "checkout", target: "test-2", type: "custom", markerEnd: arrowMarker },
  { id: "e-t0-b", source: "test-0", target: "build", type: "custom", markerEnd: arrowMarker },
  { id: "e-t1-b", source: "test-1", target: "build", type: "custom", markerEnd: arrowMarker },
  { id: "e-t2-b", source: "test-2", target: "build", type: "custom", markerEnd: arrowMarker },
  { id: "e-b-s", source: "build", target: "scan", type: "custom", markerEnd: arrowMarker },
  { id: "e-s-ds", source: "scan", target: "deploy-staging", type: "custom", markerEnd: arrowMarker },
  { id: "e-ds-a", source: "deploy-staging", target: "approval", type: "custom", markerEnd: arrowMarker },
  { id: "e-a-dp", source: "approval", target: "deploy-prod", type: "custom", markerEnd: arrowMarker },
  { id: "e-dp-n", source: "deploy-prod", target: "notify", type: "custom", markerEnd: arrowMarker },
];

function FlowCanvasInner() {
  const nodes = useEditorStore((s) => s.nodes);
  const edges = useEditorStore((s) => s.edges);
  const onNodesChange = useEditorStore((s) => s.onNodesChange);
  const onEdgesChange = useEditorStore((s) => s.onEdgesChange);
  const setEdges = useEditorStore((s) => s.setEdges);
  const setNodes = useEditorStore((s) => s.setNodes);
  const { screenToFlowPosition } = useReactFlow();

  useEffect(() => {
    if (nodes.length === 0) {
      setNodes(initialNodes);
      setEdges(initialEdges);
    }
  }, [setNodes, setEdges, nodes.length]);

  const onConnect = useCallback(
    (params: Connection) => setEdges((eds) => addEdge({ ...params, type: "custom", markerEnd: arrowMarker }, eds)),
    [setEdges],
  );

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
  }, []);

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      const raw = e.dataTransfer.getData("application/helios-step");
      if (!raw) return;
      let item: Record<string, unknown>;
      try {
        item = JSON.parse(raw);
      } catch {
        return;
      }
      const position = screenToFlowPosition({
        x: e.clientX,
        y: e.clientY,
      });
      const id = `${String(item.id ?? "step")}-${Date.now()}`;
      const newNode: Node = {
        id,
        type: String(item.nodeType ?? "step"),
        position,
        data: {
          label: String(item.name ?? "未命名"),
          icon: String(item.icon ?? "⚙"),
          ...(typeof item.defaultConfig === "object" && item.defaultConfig != null
            ? (item.defaultConfig as Record<string, unknown>)
            : {}),
        },
      };
      setNodes((nds) => [...nds, newNode]);
    },
    [screenToFlowPosition, setNodes],
  );

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onConnect={onConnect}
      onDragOver={onDragOver}
      onDrop={onDrop}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
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

export default function FlowCanvas() {
  return (
    <ReactFlowProvider>
      <FlowCanvasInner />
    </ReactFlowProvider>
  );
}
