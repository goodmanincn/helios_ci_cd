import { create } from "zustand";
import {
  applyNodeChanges,
  applyEdgeChanges,
  type Node,
  type Edge,
  type NodeChange,
  type EdgeChange,
} from "@xyflow/react";

interface EditorState {
  nodes: Node[];
  edges: Edge[];
  selectedNodeId: string | null;

  setNodes: (updater: Node[] | ((prev: Node[]) => Node[])) => void;
  setEdges: (updater: Edge[] | ((prev: Edge[]) => Edge[])) => void;
  onNodesChange: (changes: NodeChange[]) => void;
  onEdgesChange: (changes: EdgeChange[]) => void;
  setSelectedNodeId: (id: string | null) => void;
  updateNodeData: (id: string, patch: Record<string, unknown>) => void;
}

export const useEditorStore = create<EditorState>((set, get) => ({
  nodes: [],
  edges: [],
  selectedNodeId: null,

  setNodes: (updater) =>
    set((state) => ({
      nodes: typeof updater === "function" ? updater(state.nodes) : updater,
    })),

  setEdges: (updater) =>
    set((state) => ({
      edges: typeof updater === "function" ? updater(state.edges) : updater,
    })),

  onNodesChange: (changes) => {
    const next = applyNodeChanges(changes, get().nodes);
    set({ nodes: next });
    // selection change 同步到 selectedNodeId
    const selectChange = changes.find((c) => c.type === "select");
    if (selectChange) {
      set({ selectedNodeId: selectChange.selected ? selectChange.id : null });
    }
  },

  onEdgesChange: (changes) => {
    const next = applyEdgeChanges(changes, get().edges);
    set({ edges: next });
  },

  setSelectedNodeId: (id) => set({ selectedNodeId: id }),

  updateNodeData: (id, patch) => {
    set((state) => ({
      nodes: state.nodes.map((n) =>
        n.id === id ? { ...n, data: { ...n.data, ...patch } } : n,
      ),
    }));
  },
}));
