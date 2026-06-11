"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { useEditorStore } from "./editor-store";
import { yamlToGraph, graphToYaml } from "./yaml-graph";

export default function YamlTab() {
  const nodes = useEditorStore((s) => s.nodes);
  const edges = useEditorStore((s) => s.edges);
  const setNodes = useEditorStore((s) => s.setNodes);
  const setEdges = useEditorStore((s) => s.setEdges);

  const [yamlText, setYamlText] = useState("");
  const [parseError, setParseError] = useState<string | null>(null);
  const skipGraphToYaml = useRef(false);
  const skipYamlToGraph = useRef(false);
  const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Graph → YAML (画布改动)
  useEffect(() => {
    if (skipGraphToYaml.current) {
      skipGraphToYaml.current = false;
      return;
    }
    if (nodes.length === 0) return;
    const yml = graphToYaml(nodes, edges);
    skipYamlToGraph.current = true;
    setYamlText(yml);
    setParseError(null);
  }, [nodes, edges]);

  // YAML → Graph (编辑器改动)
  const onChange = useCallback(
    (text: string) => {
      setYamlText(text);
      if (debounceTimer.current) clearTimeout(debounceTimer.current);
      debounceTimer.current = setTimeout(() => {
        const result = yamlToGraph(text);
        if (result) {
          skipGraphToYaml.current = true;
          setNodes(result.nodes);
          setEdges(result.edges);
          setParseError(null);
        } else {
          setParseError("YAML 解析失败");
        }
      }, 500);
    },
    [setNodes, setEdges],
  );

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0 }}>
      {parseError && (
        <div
          style={{
            padding: "8px 12px",
            background: "rgba(248,113,113,0.1)",
            borderBottom: "1px solid rgba(248,113,113,0.3)",
            color: "var(--danger)",
            fontSize: 12,
          }}
        >
          {parseError}
        </div>
      )}
      <textarea
        value={yamlText}
        onChange={(e) => onChange(e.target.value)}
        spellCheck={false}
        style={{
          flex: 1,
          width: "100%",
          padding: 16,
          background: "var(--bg-elev)",
          color: "var(--fg)",
          fontSize: 13,
          fontFamily: "var(--font-mono), monospace",
          lineHeight: 1.6,
          border: "none",
          outline: "none",
          resize: "none",
          whiteSpace: "pre",
          overflow: "auto",
        }}
      />
    </div>
  );
}
