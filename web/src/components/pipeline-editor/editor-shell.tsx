"use client";

import { useState } from "react";
import FlowCanvas from "./flow-canvas";
import StepLibraryPanel from "./step-library-panel";
import PropertyPanel from "./property-panel";
import YamlTab from "./yaml-tab";
import HistoryTab from "./history-tab";
import { useEditorStore } from "./editor-store";
import { graphToYaml } from "./yaml-graph";
import { updatePipelineSpec } from "@/lib/pipelines-api";
import { useAuthStore } from "@/lib/auth-store";

type TabKey = "canvas" | "yaml" | "triggers" | "variables" | "history";

const TABS: { key: TabKey; label: string }[] = [
  { key: "canvas", label: "画布" },
  { key: "yaml", label: "YAML" },
  { key: "triggers", label: "触发器" },
  { key: "variables", label: "变量" },
  { key: "history", label: "历史" },
];

export default function EditorShell({ pipelineId }: { pipelineId: string }) {
  const [tab, setTab] = useState<TabKey>("canvas");
  const validationErrors = useEditorStore((s) => s.validationErrors);
  const hasErrors = validationErrors.length > 0;
  const nodes = useEditorStore((s) => s.nodes);
  const edges = useEditorStore((s) => s.edges);
  const token = useAuthStore((s) => s.accessToken);
  const [saving, setSaving] = useState(false);

  async function handleSave() {
    if (hasErrors || nodes.length === 0) return;
    const message = window.prompt("保存备注 (可选):", "") ?? "";
    const specRaw = graphToYaml(nodes, edges);
    setSaving(true);
    try {
      await updatePipelineSpec(token, pipelineId, { spec_raw: specRaw, message });
      alert("保存成功");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "保存失败";
      alert(msg);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        overflow: "hidden",
      }}
    >
      {/* 顶部标题行 */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          padding: "14px 24px",
          borderBottom: "1px solid var(--border)",
          flexShrink: 0,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <h1
              style={{
                fontSize: 16,
                fontWeight: 590,
                color: "var(--fg)",
                margin: 0,
              }}
            >
              build-and-deploy
            </h1>
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 6,
                fontSize: 11,
                padding: "2px 8px",
                background: "rgba(52,211,153,0.1)",
                color: "var(--success)",
                borderRadius: 4,
                border: "1px solid rgba(52,211,153,0.25)",
              }}
            >
              <span
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: "50%",
                  background: "var(--success)",
                  display: "inline-block",
                }}
              />
              v3 · 已启用
            </span>
          </div>
          <div
            style={{
              fontSize: 11,
              color: "var(--fg-dim)",
              marginTop: 2,
            }}
          >
            api-gateway · 最后保存 5分钟前 · alice
          </div>
        </div>

        {/* Tab bar */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 2,
            background: "rgba(255,255,255,0.03)",
            border: "1px solid var(--border)",
            borderRadius: 6,
            padding: 2,
          }}
        >
          {TABS.map((t) => (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              className="btn bsm"
              style={{
                padding: "4px 10px",
                fontSize: 12,
                borderRadius: 5,
                border: "none",
                background:
                  tab === t.key
                    ? "rgba(94,106,210,0.15)"
                    : "transparent",
                color: tab === t.key ? "var(--fg)" : "var(--fg-mute)",
              }}
            >
              {t.label}
            </button>
          ))}
        </div>

        <button className="btn bsm">▶ 试运行</button>
        <button
          className="btn bsm btn-primary"
          disabled={hasErrors || saving || nodes.length === 0}
          title={hasErrors ? "请先修复 YAML 错误" : undefined}
          onClick={handleSave}
        >
          {saving ? "保存中…" : "保存并启用"}
        </button>
      </div>

      {/* 主内容区 */}
      {tab === "canvas" && (
        <div
          style={{
            flex: 1,
            display: "grid",
            gridTemplateColumns: "200px 1fr 320px",
            minHeight: 0,
            overflow: "hidden",
          }}
        >
          <aside
            style={{
              borderRight: "1px solid var(--border)",
              padding: 14,
              overflow: "hidden",
              background: "var(--bg-elev)",
              display: "flex",
              flexDirection: "column",
            }}
          >
            <StepLibraryPanel />
          </aside>

          <div style={{ position: "relative", overflow: "hidden", minWidth: 0 }}>
            <FlowCanvas />
          </div>

          <aside
            style={{
              borderLeft: "1px solid var(--border)",
              padding: 18,
              overflow: "auto",
              background: "var(--bg-elev)",
            }}
          >
            <PropertyPanel />
          </aside>
        </div>
      )}

      {tab === "yaml" && <YamlTab />}
      {tab === "history" && <HistoryTab pipelineId={pipelineId} />}

      {tab !== "canvas" && tab !== "yaml" && tab !== "history" && (
        <div
          style={{
            flex: 1,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: "var(--fg-dim)",
            fontSize: 14,
          }}
        >
          「{TABS.find((t) => t.key === tab)?.label}」tab 待实现 (pipeline #{pipelineId})
        </div>
      )}
    </div>
  );
}
