import { apiFetch } from "./api";

export interface Host {
  id: number;
  org_id: number;
  name: string;
  ip: string;
  ssh_port: number;
  ssh_user: string;
  credential_id?: number;
  labels?: Record<string, string>;
  status: string;
  os?: string;
  arch?: string;
  last_heartbeat?: string;
  created_at: string;
}

export interface HostTestResult {
  reachable: boolean;
  ssh_ok: boolean;
  uname?: string;
  message?: string;
}

export async function listHosts(token: string | null, filters?: { q?: string; label?: string }) {
  const qs = new URLSearchParams();
  if (filters?.q) qs.set("q", filters.q);
  if (filters?.label) qs.set("label", filters.label);
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiFetch<Host[]>(`/api/v1/hosts${suffix}`, { token });
}

export async function getHost(token: string | null, id: number | string) {
  return apiFetch<Host>(`/api/v1/hosts/${id}`, { token });
}

export async function createHost(
  token: string | null,
  body: { name: string; ip: string; ssh_port?: number; ssh_user?: string; credential_id?: number; labels?: Record<string, string> },
) {
  return apiFetch<Host>("/api/v1/hosts", {
    token,
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function updateHost(
  token: string | null,
  id: number | string,
  body: Partial<Pick<Host, "name" | "ip" | "ssh_port" | "ssh_user" | "credential_id" | "labels">>,
) {
  return apiFetch<Host>(`/api/v1/hosts/${id}`, {
    token,
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export async function deleteHost(token: string | null, id: number | string) {
  return apiFetch<null>(`/api/v1/hosts/${id}`, { token, method: "DELETE" });
}

export async function testHost(token: string | null, id: number | string) {
  return apiFetch<HostTestResult>(`/api/v1/hosts/${id}/test`, { token, method: "POST" });
}
