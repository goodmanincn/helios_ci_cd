// 流水线模板市场 API (M8 T8.2.1).
import { apiFetch } from "./api";

export interface PipelineTemplate {
  id: number;
  slug: string;
  name: string;
  description?: string;
  category?: string;
  tags?: string[];
  builtin: boolean;
  org_id?: number | null;
  spec_raw?: string;
  created_at?: string;
  updated_at?: string;
}

export interface CloneFromTemplateResult {
  pipeline_id: number;
  version_id: number;
  version: number;
  pipeline_name: string;
  template_slug: string;
}

export async function listTemplates(
  token: string | null,
  filters?: { q?: string; category?: string; tag?: string },
) {
  const qs = new URLSearchParams();
  if (filters?.q) qs.set("q", filters.q);
  if (filters?.category) qs.set("category", filters.category);
  if (filters?.tag) qs.set("tag", filters.tag);
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiFetch<PipelineTemplate[]>(`/api/v1/pipeline-templates${suffix}`, { token });
}

export async function getTemplate(token: string | null, id: number | string) {
  return apiFetch<PipelineTemplate>(`/api/v1/pipeline-templates/${id}`, { token });
}

export async function cloneFromTemplate(
  token: string | null,
  body: {
    template_slug?: string;
    template_id?: number;
    project_id: number;
    name: string;
    description?: string;
  },
) {
  return apiFetch<CloneFromTemplateResult>("/api/v1/pipelines/from-template", {
    token,
    method: "POST",
    body: JSON.stringify(body),
  });
}
