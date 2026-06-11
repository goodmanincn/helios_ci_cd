"use client";

import { useParams } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import EditorShell from "@/components/pipeline-editor/editor-shell";

export default function PipelineEditPage() {
  const params = useParams();
  const id = String(params.id ?? "");

  return (
    <AppShell
      title="流水线"
      contentClassName="overflow-hidden"
    >
      <EditorShell pipelineId={id} />
    </AppShell>
  );
}
