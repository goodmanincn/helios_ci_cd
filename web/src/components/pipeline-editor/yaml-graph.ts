import yaml from "js-yaml";
import { type Node, type Edge } from "@xyflow/react";
import { type PipelineSpec, type Stage } from "@/lib/pipelines-api";

const NODE_WIDTH = 200;
const NODE_HEIGHT = 60;
const GAP_X = 120;
const GAP_Y = 40;

/** 从 edges 构建邻接表: target -> sources (即 needs) */
function buildNeedsMap(edges: Edge[]): Map<string, string[]> {
  const map = new Map<string, string[]>();
  for (const e of edges) {
    const arr = map.get(e.target) ?? [];
    arr.push(e.source);
    map.set(e.target, arr);
  }
  return map;
}

/** 计算每个节点的拓扑深度 (无 needs = 0) */
function computeDepths(nodes: Node[], edges: Edge[]): Map<string, number> {
  const needsMap = buildNeedsMap(edges);
  const depthMap = new Map<string, number>();

  function getDepth(id: string): number {
    if (depthMap.has(id)) return depthMap.get(id)!;
    const needs = needsMap.get(id) ?? [];
    if (needs.length === 0) {
      depthMap.set(id, 0);
      return 0;
    }
    const d = Math.max(...needs.map(getDepth)) + 1;
    depthMap.set(id, d);
    return d;
  }

  for (const n of nodes) getDepth(n.id);
  return depthMap;
}

/** 简单网格布局: 按深度分 x, 同深度按原始顺序分 y */
function autoLayout(nodes: Node[], edges: Edge[]): Node[] {
  const depths = computeDepths(nodes, edges);
  const byDepth = new Map<number, Node[]>();
  for (const n of nodes) {
    const d = depths.get(n.id) ?? 0;
    const arr = byDepth.get(d) ?? [];
    arr.push(n);
    byDepth.set(d, arr);
  }

  const sortedDepths = Array.from(byDepth.keys()).sort((a, b) => a - b);
  return nodes.map((n) => {
    const d = depths.get(n.id) ?? 0;
    const layer = byDepth.get(d)!;
    const idx = layer.indexOf(n);
    return {
      ...n,
      position: {
        x: 80 + d * (NODE_WIDTH + GAP_X),
        y: 30 + idx * (NODE_HEIGHT + GAP_Y),
      },
    };
  });
}

// —— YAML → Graph ——

export function yamlToGraph(yamlText: string): { nodes: Node[]; edges: Edge[] } | null {
  let spec: PipelineSpec;
  try {
    spec = yaml.load(yamlText) as PipelineSpec;
  } catch {
    return null;
  }
  if (!spec || !Array.isArray(spec.stages)) return null;

  const nodes: Node[] = spec.stages.map((stage: Stage) => {
    const nodeType =
      stage.type === "approval"
        ? "approval"
        : stage.matrix != null
          ? "matrix"
          : "step";

    const data: Record<string, unknown> = {
      label: stage.name || stage.id,
      ...stage,
    };

    // 特殊字段映射到 UI
    if (stage.type === "approval") {
      data.approvers = Array.isArray(stage.approvers)
        ? stage.approvers.join(", ")
        : stage.approvers;
    }

    return {
      id: stage.id,
      type: nodeType,
      position: { x: 0, y: 0 }, // 布局后重算
      data,
    };
  });

  const edges: Edge[] = [];
  for (const stage of spec.stages) {
    if (stage.needs) {
      for (const src of stage.needs) {
        edges.push({
          id: `e-${src}-${stage.id}`,
          source: src,
          target: stage.id,
          type: "custom",
          markerEnd: { type: "arrowclosed", color: "#5e6ad2" },
        });
      }
    }
  }

  return { nodes: autoLayout(nodes, edges), edges };
}

// —— Graph → YAML ——

export function graphToYaml(nodes: Node[], edges: Edge[]): string {
  const needsMap = buildNeedsMap(edges);

  // 按 y 坐标从上到下排序, 同 y 按 x 排序
  const sorted = [...nodes].sort((a, b) => {
    if (a.position.y !== b.position.y) return a.position.y - b.position.y;
    return a.position.x - b.position.x;
  });

  const stages: Stage[] = sorted.map((n) => {
    const data = n.data as Record<string, unknown>;
    const needs = needsMap.get(n.id) ?? undefined;

    // 从 data 还原 stage 字段 (去掉 UI 字段)
    const {
      label,
      icon,
      sub,
      approvers: approversStr,
      ...stageRest
    } = data;

    const stage: Stage = {
      id: n.id,
      ...(stageRest as Record<string, unknown>),
    } as Stage;

    if (needs && needs.length > 0) {
      stage.needs = needs;
    }

    // approval approvers 字符串转数组
    if (n.type === "approval" && typeof approversStr === "string") {
      stage.approvers = approversStr.split(",").map((s) => s.trim());
    }

    // name 回写
    if (typeof label === "string") {
      stage.name = label;
    }

    return stage;
  });

  const spec: PipelineSpec = {
    version: "1",
    name: "pipeline",
    stages,
  };

  return yaml.dump(spec, {
    indent: 2,
    lineWidth: -1,
    noRefs: true,
    sortKeys: false,
  });
}
