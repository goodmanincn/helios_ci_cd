// Runs API — 复用 apiFetch (T1.6.1 后端: GET /api/v1/runs, /api/v1/runs/:id)
import { apiFetch } from "./api";

export type RunStatus =
  | "pending"
  | "running"
  | "approval"
  | "success"
  | "failed"
  | "canceled"
  | "timeout";

export interface ProjectSummary {
  id: number;
  slug: string;
  name: string;
  repo_url?: string;
  repo_type?: string;
}

export interface Step {
  id: number;
  name?: string;
  index?: number;
  status?: string;
  exit_code?: number;
  log_object?: string;
  log_size?: number;
  started_at?: string;
  finished_at?: string;
  duration_ms?: number;
}

export interface Stage {
  id: number;
  stage_id: string;
  name?: string;
  status?: string;
  started_at?: string;
  finished_at?: string;
  duration_ms?: number;
  steps: Step[];
}

// T2.6.4: 审批 (E2.6)
export type ApprovalStatus = "pending" | "approved" | "rejected" | "timeout" | "canceled";

export interface ApprovalVote {
  id: number;
  request_id: number;
  user_id?: number;
  username: string;
  decision: "approve" | "reject";
  comment?: string;
  created_at: string;
}

export interface ApprovalRequest {
  id: number;
  run_id: number;
  stage_id: string;
  required_approvers: string[];
  mode: "any" | "all";
  status: ApprovalStatus;
  on_timeout: "reject" | "approve" | "pause";
  timeout_at?: string;
  created_at: string;
  updated_at: string;
  approvals: ApprovalVote[];
}

export interface RunListItem {
  id: number;
  number: number;
  status: RunStatus | string;
  branch?: string;
  commit_sha?: string;
  message?: string;
  trigger_type?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  duration_ms?: number;
  project?: ProjectSummary;
}

export interface RunDetail extends RunListItem {
  pipeline_id: number;
  version_id: number;
  stages: Stage[];
  approval_requests?: ApprovalRequest[];
}

export interface RunListResult {
  items: RunListItem[];
  limit: number;
  next_id: number | null;
}

export interface RunListOpts {
  project_id?: number;
  pipeline_id?: number;
  branch?: string;
  status?: string;
  limit?: number;
  before_id?: number;
}

export async function listRuns(token: string, opts: RunListOpts = {}): Promise<RunListResult> {
  const qs = new URLSearchParams();
  if (opts.project_id != null) qs.set("project_id", String(opts.project_id));
  if (opts.pipeline_id != null) qs.set("pipeline_id", String(opts.pipeline_id));
  if (opts.branch) qs.set("branch", opts.branch);
  if (opts.status) qs.set("status", opts.status);
  if (opts.limit != null) qs.set("limit", String(opts.limit));
  if (opts.before_id != null) qs.set("before_id", String(opts.before_id));
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiFetch<RunListResult>(`/api/v1/runs${suffix}`, { token });
}

export async function getRun(token: string, id: number): Promise<RunDetail> {
  return apiFetch<RunDetail>(`/api/v1/runs/${id}`, { token });
}

// cancel/retry (T1.6.4) — POST 操作, 返回新状态摘要
export interface CancelResult {
  id: number;
  status: string;
}

export async function cancelRun(token: string, id: number): Promise<CancelResult> {
  return apiFetch<CancelResult>(`/api/v1/runs/${id}/cancel`, {
    token,
    method: "POST",
  });
}

export interface RetryResult {
  id: number; // 新 run id
  number: number; // 新 run number
  status: string; // "pending"
  origin_run_id: number;
  task_id?: string;
}

export async function retryRun(token: string, id: number): Promise<RetryResult> {
  return apiFetch<RetryResult>(`/api/v1/runs/${id}/retry`, {
    token,
    method: "POST",
  });
}

// T2.6.4: 审批投票
export interface ApprovalVoteResult {
  request_id: number;
  request_status: ApprovalStatus;
  vote_id: number;
  decision: "approve" | "reject";
  username: string;
  next_run_status: string; // "" / "running" / "failed"
}

export async function approveStage(
  token: string,
  runId: number,
  stageId: string,
  comment: string,
): Promise<ApprovalVoteResult> {
  return apiFetch<ApprovalVoteResult>(
    `/api/v1/runs/${runId}/approvals/${encodeURIComponent(stageId)}/approve`,
    { token, method: "POST", body: JSON.stringify({ comment }) },
  );
}

export async function rejectStage(
  token: string,
  runId: number,
  stageId: string,
  comment: string,
): Promise<ApprovalVoteResult> {
  return apiFetch<ApprovalVoteResult>(
    `/api/v1/runs/${runId}/approvals/${encodeURIComponent(stageId)}/reject`,
    { token, method: "POST", body: JSON.stringify({ comment }) },
  );
}

// ===== UI helpers =====

export function statusBadgeColor(s: string): { fg: string; bg: string; label: string } {
  switch (s) {
    case "success":
      return { fg: "#5eecaa", bg: "rgba(34,197,94,0.14)", label: "成功" };
    case "running":
      return { fg: "#fbd55a", bg: "rgba(250,204,21,0.14)", label: "运行中" };
    case "approval":
      return { fg: "#facc15", bg: "rgba(250,204,21,0.14)", label: "待审批" };
    case "pending":
      return { fg: "#9aa5b3", bg: "rgba(148,163,184,0.16)", label: "排队中" };
    case "failed":
      return { fg: "#fb7185", bg: "rgba(248,113,113,0.14)", label: "失败" };
    case "timeout":
      return { fg: "#fb923c", bg: "rgba(251,146,60,0.14)", label: "超时" };
    case "canceled":
      return { fg: "#a1a1aa", bg: "rgba(161,161,170,0.14)", label: "已取消" };
    default:
      return { fg: "var(--fg)", bg: "var(--bg-elev-2)", label: s };
  }
}

export function fmtDuration(ms?: number): string {
  if (!ms || ms <= 0) return "-";
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rs = Math.floor(s % 60);
  return `${m}m ${rs}s`;
}

export function fmtTime(s?: string): string {
  if (!s) return "-";
  try {
    return new Date(s).toLocaleString();
  } catch {
    return s;
  }
}

export function shortSHA(s?: string): string {
  return s ? s.slice(0, 7) : "-";
}
