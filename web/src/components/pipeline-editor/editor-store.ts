import { create } from "zustand";
import {
  applyNodeChanges,
  applyEdgeChanges,
  type Node,
  type Edge,
  type NodeChange,
  type EdgeChange,
} from "@xyflow/react";
import { type ValidateError } from "@/lib/pipelines-api";

interface EditorState {
  nodes: Node[];
  edges: Edge[];
  selectedNodeId: string | null;
  validationErrors: ValidateError[];
  validationLoading: boolean;
  nodeErrorMap: Record<string, ValidateError[]>;

  setNodes: (updater: Node[] | ((prev: Node[]) => Node[])) => void;
  setEdges: (updater: Edge[] | ((prev: Edge[]) => Edge[])) => void;
  onNodesChange: (changes: NodeChange[]) => void;
  onEdgesChange: (changes: EdgeChange[]) => void;
  setSelectedNodeId: (id: string | null) => void;
  updateNodeData: (id: string, patch: Record<string, unknown>) => void;
  setValidationErrors: (errors: ValidateError[]) => void;
  setValidationLoading: (loading: boolean) => void;
  setNodeErrorMap: (map: Record<string, ValidateError[]>) => void;
  removeNode: (id: string) => void;
  duplicateNode: (id: string) => void;
}

export const useEditorStore = create<EditorState>((set, get) => ({
  nodes: [],
  edges: [],
  selectedNodeId: null,
  validationErrors: [],
  validationLoading: false,
  nodeErrorMap: {},

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

  setValidationErrors: (errors) => set({ validationErrors: errors }),
  setValidationLoading: (loading) => set({ validationLoading: loading }),
  setNodeErrorMap: (map) => set({ nodeErrorMap: map }),

  removeNode: (id) => {
    set((state) => ({
      nodes: state.nodes.filter((n) => n.id !== id),
      edges: state.edges.filter((e) => e.source !== id && e.target !== id),
      selectedNodeId: state.selectedNodeId === id ? null : state.selectedNodeId,
    }));
  },

  duplicateNode: (id) => {
    set((state) => {
      const src = state.nodes.find((n) => n.id === id);
      if (!src) return state;
      const newId = `${id}-copy-${Date.now()}`;
      const newNode: Node = {
        ...src,
        id: newId,
        position: { x: src.position.x + 20, y: src.position.y + 20 },
        selected: false,
      };
      return {
        nodes: [...state.nodes, newNode],
        selectedNodeId: newId,
      };
    });
  },
}));
