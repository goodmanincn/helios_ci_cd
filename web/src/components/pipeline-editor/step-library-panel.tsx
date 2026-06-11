"use client";

import { useState } from "react";
import { STEP_LIBRARY, searchSteps, type StepLibraryItem } from "@/lib/step-library";

function StepPaletteItem({ item }: { item: StepLibraryItem }) {
  function onDragStart(e: React.DragEvent) {
    e.dataTransfer.setData("application/helios-step", JSON.stringify(item));
    e.dataTransfer.effectAllowed = "copy";
  }

  return (
    <div
      draggable
      onDragStart={onDragStart}
      style={{
        padding: "6px 10px",
        fontSize: 12,
        color: "var(--fg-mute)",
        borderRadius: 5,
        cursor: "grab",
        marginBottom: 2,
        border: "1px solid transparent",
        display: "flex",
        alignItems: "center",
        gap: 8,
        userSelect: "none",
      }}
      onMouseEnter={(e) => {
        const t = e.currentTarget;
        t.style.background = "rgba(255,255,255,0.03)";
        t.style.borderColor = "var(--border)";
        t.style.color = "var(--fg)";
      }}
      onMouseLeave={(e) => {
        const t = e.currentTarget;
        t.style.background = "transparent";
        t.style.borderColor = "transparent";
        t.style.color = "var(--fg-mute)";
      }}
    >
      <span style={{ fontSize: 14, flexShrink: 0 }}>{item.icon}</span>
      <span style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
        {item.name}
      </span>
    </div>
  );
}

export default function StepLibraryPanel() {
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const allMatched = searchSteps(query);
  const matchedIds = new Set(allMatched.map((s) => s.id));

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <input
        type="text"
        placeholder="搜索步骤…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        className="input"
        style={{
          fontSize: 12,
          padding: "6px 10px",
          marginBottom: 14,
        }}
      />
      <div style={{ flex: 1, overflowY: "auto" }}>
        {STEP_LIBRARY.map((cat) => {
          const visible = cat.items.filter((i) => matchedIds.has(i.id));
          if (visible.length === 0) return null;
          const isCollapsed = collapsed[cat.key];

          return (
            <div key={cat.key} style={{ marginBottom: 12 }}>
              <button
                type="button"
                onClick={() =>
                  setCollapsed((prev) => ({ ...prev, [cat.key]: !prev[cat.key] }))
                }
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 4,
                  width: "100%",
                  padding: "0 0 6px",
                  background: "transparent",
                  border: "none",
                  color: "var(--fg-dim)",
                  fontSize: 11,
                  fontWeight: 510,
                  textTransform: "uppercase",
                  letterSpacing: "0.4px",
                  cursor: "pointer",
                  textAlign: "left",
                }}
              >
                <span
                  style={{
                    display: "inline-block",
                    transform: isCollapsed ? "rotate(-90deg)" : "rotate(0deg)",
                    transition: "transform 0.15s",
                    fontSize: 10,
                  }}
                >
                  ▼
                </span>
                {cat.label}
              </button>
              {!isCollapsed && visible.map((item) => (
                <StepPaletteItem key={item.id} item={item} />
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}
