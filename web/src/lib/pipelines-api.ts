import { apiFetch } from "./api";

// —— Pipeline 模型 (对齐 api/internal/model) ——

export interface Pipeline {
  id: number;
  project_id: number;
  name: string;
  description?: string;
  current_version_id?: number;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface PipelineVersion {
  id: number;
  pipeline_id: number;
  version: number;
  spec: PipelineSpec;
  spec_raw: string;
  message?: string;
  created_by?: number;
  created_at: string;
}

// —— DSL 结构 (对齐 api/pkg/dsl/types.go) ——

export interface PipelineSpec {
  version: string;
  name: string;
  description?: string;
  triggers?: Trigger[];
  env?: Record<string, string>;
  variables?: Record<string, string>;
  stages: Stage[];
}

export interface Trigger {
  on: string;
  branches?: string[];
  tags?: string[];
  paths?: string[];
  paths_ignore?: string[];
  cron?: string;
  timezone?: string;
  inputs?: Record<string, Input>;
}

export interface Input {
  type: string;
  description?: string;
  options?: string[];
  default?: unknown;
  required?: boolean;
}

export interface Stage {
  id: string;
  name?: string;
  needs?: string[];
  type?: "" | "approval";
  if?: string;
  runs_on?: RunsOn;
  matrix?: Matrix;
  steps?: Step[];
  uses?: string;
  with?: Record<string, unknown>;
  env?: Record<string, string>;
  secrets?: string[];
  outputs?: Record<string, string>;
  services?: Record<string, Service>;
  // approval
  approvers?: string[];
  mode?: "any" | "all" | "quorum";
  timeout?: string;
  on_timeout?: "reject" | "approve" | "pause";
}

export interface RunsOn {
  type?: string;
  image?: string;
  labels?: string[];
  arch?: string;
  service?: string;
}

export interface Matrix {
  dimensions?: Record<string, unknown[]>;
  include?: Record<string, unknown>[];
  exclude?: Record<string, unknown>[];
}

export interface Step {
  id?: string;
  name?: string;
  if?: string;
  run?: string;
  uses?: string;
  with?: Record<string, unknown>;
  env?: Record<string, string>;
  working_directory?: string;
  shell?: string;
  continue_on_error?: boolean;
  timeout_minutes?: number;
}

export interface Service {
  image: string;
  env?: Record<string, string>;
  ports?: string[];
  cmd?: string[];
}

// —— 校验 ——

export interface ValidateError {
  kind: "syntax" | "schema" | "semantic";
  message: string;
  path?: string;
  line?: number;
  column?: number;
}

export interface ValidateResult {
  valid: boolean;
  errors: ValidateError[];
  warnings: ValidateError[];
  pipeline?: PipelineSpec;
  summary?: {
    name?: string;
    version?: string;
    stage_count: number;
    stage_ids?: string[];
  };
}

export async function validateSpec(
  token: string | null,
  specRaw: string,
): Promise<ValidateResult> {
  return apiFetch<ValidateResult>("/api/v1/pipelines/validate", {
    token,
    method: "POST",
    body: JSON.stringify({ spec_raw: specRaw }),
  });
}

// —— Pipeline 版本 API ——

export async function getPipelineVersions(
  token: string | null,
  pipelineId: number | string,
) {
  return apiFetch<PipelineVersion[]>(`/api/v1/pipelines/${pipelineId}/versions`, {
    token,
  });
}

export async function getPipelineVersion(
  token: string | null,
  pipelineId: number | string,
  version: number,
) {
  return apiFetch<PipelineVersion>(
    `/api/v1/pipelines/${pipelineId}/versions/${version}`,
    { token },
  );
}

export async function updatePipelineSpec(
  token: string | null,
  pipelineId: number | string,
  body: { spec_raw: string; message?: string; base_version_id?: number },
) {
  return apiFetch<PipelineVersion>(`/api/v1/pipelines/${pipelineId}/spec`, {
    token,
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export async function restorePipelineVersion(
  token: string | null,
  pipelineId: number | string,
  version: number,
) {
  return apiFetch<PipelineVersion>(
    `/api/v1/pipelines/${pipelineId}/versions/${version}/restore`,
    { token, method: "POST" },
  );
}
