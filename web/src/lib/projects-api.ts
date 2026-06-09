// Projects API — 复用 apiFetch (token 参数走 Bearer)。
import { apiFetch } from "./api";

export type Visibility = "private" | "public";
export type RepoType = "github" | "gitlab" | "gitee" | "bitbucket";

export interface Project {
  id: number;
  org_id: number;
  name: string;
  slug: string;
  description?: string;
  repo_url: string;
  repo_type: RepoType;
  default_branch: string;
  visibility: Visibility;
  created_by?: number;
  created_at: string;
  updated_at: string;
}

export interface ProjectListResult {
  items: Project[];
  total: number;
  limit: number;
  offset: number;
}

export interface CreateProjectInput {
  name: string;
  slug: string;
  description?: string;
  repo_url: string;
  repo_type: RepoType;
  default_branch?: string;
  visibility?: Visibility;
}

export interface UpdateProjectInput {
  name?: string;
  description?: string;
  default_branch?: string;
  visibility?: Visibility;
}

export async function listProjects(
  token: string,
  opts: { q?: string; limit?: number; offset?: number } = {},
): Promise<ProjectListResult> {
  const qs = new URLSearchParams();
  if (opts.q) qs.set("q", opts.q);
  if (opts.limit != null) qs.set("limit", String(opts.limit));
  if (opts.offset != null) qs.set("offset", String(opts.offset));
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiFetch<ProjectListResult>(`/api/v1/projects${suffix}`, { token });
}

export async function getProject(token: string, id: number): Promise<Project> {
  return apiFetch<Project>(`/api/v1/projects/${id}`, { token });
}

export async function createProject(
  token: string,
  input: CreateProjectInput,
): Promise<Project> {
  return apiFetch<Project>("/api/v1/projects", {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export async function updateProject(
  token: string,
  id: number,
  patch: UpdateProjectInput,
): Promise<Project> {
  return apiFetch<Project>(`/api/v1/projects/${id}`, {
    method: "PATCH",
    token,
    body: JSON.stringify(patch),
  });
}

export async function deleteProject(token: string, id: number): Promise<void> {
  // 204 No Content — apiFetch 已经处理空响应
  await apiFetch<null>(`/api/v1/projects/${id}`, { method: "DELETE", token });
}

export async function syncProject(token: string, id: number): Promise<{ message: string }> {
  return apiFetch<{ message: string }>(`/api/v1/projects/${id}/sync`, {
    method: "POST",
    token,
  });
}
