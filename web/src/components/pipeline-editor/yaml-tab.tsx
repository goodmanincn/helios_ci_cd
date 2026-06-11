"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { useEditorStore } from "./editor-store";
import { yamlToGraph, graphToYaml } from "./yaml-graph";
import { validateSpec } from "@/lib/pipelines-api";
import { useAuthStore } from "@/lib/auth-store";

export default function YamlTab() {
  const nodes = useEditorStore((s) => s.nodes);
  const edges = useEditorStore((s) => s.edges);
  const setNodes = useEditorStore((s) => s.setNodes);
  const setEdges = useEditorStore((s) => s.setEdges);
  const setValidationErrors = useEditorStore((s) => s.setValidationErrors);
  const setValidationLoading = useEditorStore((s) => s.setValidationLoading);
  const token = useAuthStore((s) => s.accessToken);

  const [yamlText, setYamlText] = useState("");
  const [parseError, setParseError] = useState<string | null>(null);
  const skipGraphToYaml = useRef(false);
  const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Graph → YAML (画布改动)
  useEffect(() => {
    if (skipGraphToYaml.current) {
      skipGraphToYaml.current = false;
      return;
    }
    if (nodes.length === 0) return;
    const yml = graphToYaml(nodes, edges);
    setYamlText(yml);
    setParseError(null);
  }, [nodes, edges]);

  // YAML → Graph + 校验 (编辑器改动)
  const onChange = useCallback(
    (text: string) => {
      setYamlText(text);
      if (debounceTimer.current) clearTimeout(debounceTimer.current);
      debounceTimer.current = setTimeout(async () => {
        // 1. 解析更新画布
        const result = yamlToGraph(text);
        if (result) {
          skipGraphToYaml.current = true;
          setNodes(result.nodes);
          setEdges(result.edges);
          setParseError(null);
        } else {
          setParseError("YAML 解析失败");
          setValidationErrors([]);
          return;
        }

        // 2. 调后端校验 API
        setValidationLoading(true);
        try {
          const res = await validateSpec(token, text);
          setValidationErrors(res.errors ?? []);
        } catch {
          // 网络错误静默,不阻断编辑
          setValidationErrors([]);
        } finally {
          setValidationLoading(false);
        }
      }, 500);
    },
    [setNodes, setEdges, setValidationErrors, setValidationLoading, token],
  );

  const validationErrors = useEditorStore((s) => s.validationErrors);

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0 }}>
      {(parseError || validationErrors.length > 0) && (
        <div
          style={{
            padding: "8px 12px",
            background: "rgba(248,113,113,0.1)",
            borderBottom: "1px solid rgba(248,113,113,0.3)",
            color: "var(--danger)",
            fontSize: 12,
            maxHeight: 120,
            overflow: "auto",
          }}
        >
          {parseError && <div>{parseError}</div>}
          {validationErrors.map((e, i) => (
            <div key={i} style={{ marginTop: 2 }}>
              {e.line ? `L${e.line}: ` : ""}
              {e.message}
            </div>
          ))}
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
