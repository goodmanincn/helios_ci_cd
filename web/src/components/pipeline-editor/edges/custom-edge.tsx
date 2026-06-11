"use client";

import { BaseEdge, getSmoothStepPath, type EdgeProps } from "@xyflow/react";

export interface CustomEdgeData {
  style?: "normal" | "conditional" | "failure";
}

export default function CustomEdge({
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  markerEnd,
}: EdgeProps) {
  const [edgePath] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    borderRadius: 8,
  });

  const styleType = (data as CustomEdgeData | undefined)?.style ?? "normal";

  const stroke =
    styleType === "failure"
      ? "#f87171"
      : styleType === "conditional"
        ? "#7170ff"
        : "#5e6ad2";

  const strokeDasharray = styleType === "conditional" ? "4,3" : undefined;

  return (
    <BaseEdge
      path={edgePath}
      markerEnd={markerEnd}
      style={{
        stroke,
        strokeWidth: 1.5,
        strokeDasharray,
      }}
    />
  );
}
