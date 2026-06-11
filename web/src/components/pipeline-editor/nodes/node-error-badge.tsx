"use client";

import { useState } from "react";
import { useEditorStore } from "../editor-store";

export default function NodeErrorBadge({ nodeId }: { nodeId: string }) {
  const nodeErrorMap = useEditorStore((s) => s.nodeErrorMap);
  const errors = nodeErrorMap[nodeId];
  if (!errors || errors.length === 0) return null;

  return <ErrorBadgeInner errors={errors} />;
}

function ErrorBadgeInner({ errors }: { errors: { message: string; line?: number }[] }) {
  const [show, setShow] = useState(false);

  return (
    <div
      style={{ position: "absolute", top: -6, right: -6, zIndex: 10 }}
      onMouseEnter={() => setShow(true)}
      onMouseLeave={() => setShow(false)}
    >
      <div
        style={{
          width: 16,
          height: 16,
          borderRadius: "50%",
          background: "var(--danger)",
          color: "white",
          fontSize: 11,
          fontWeight: 700,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          cursor: "help",
          boxShadow: "0 2px 4px rgba(0,0,0,0.4)",
        }}
      >
        !
      </div>
      {show && (
        <div
          style={{
            position: "absolute",
            top: 18,
            right: 0,
            width: 220,
            background: "var(--bg-elev-2)",
            border: "1px solid var(--border)",
            borderRadius: 6,
            padding: "8px 10px",
            fontSize: 11,
            color: "var(--fg)",
            boxShadow: "0 8px 24px rgba(0,0,0,0.5)",
          }}
        >
          {errors.map((e, i) => (
            <div key={i} style={{ marginBottom: i < errors.length - 1 ? 4 : 0 }}>
              {e.line ? `L${e.line}: ` : ""}
              {e.message}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
