import { apiFetch } from "./api";

export interface Cluster {
  id: number;
  org_id: number;
  name: string;
  provider: string;
  region?: string;
  endpoint?: string;
  status: string;
  last_health_check?: string;
  created_at: string;
}

export interface HealthInfo {
  version: string;
  node_count: number;
  namespace_count: number;
  healthy: boolean;
}

export interface WorkloadInfo {
  kind: string;
  name: string;
  namespace: string;
  ready: number;
  desired: number;
  updated: number;
  status: string;
}

export interface EventInfo {
  type: string;
  reason: string;
  message: string;
  object: string;
  timestamp: string;
}

export interface RevisionInfo {
  revision: number;
  image: string;
  status: string;
  created_at: string;
}

export async function listClusters(token: string | null) {
  return apiFetch<Cluster[]>("/api/v1/clusters", { token });
}

export async function getCluster(token: string | null, id: number | string) {
  return apiFetch<Cluster>(`/api/v1/clusters/${id}`, { token });
}

export async function createCluster(
  token: string | null,
  body: { name: string; provider: string; region?: string; kubeconfig?: string },
) {
  return apiFetch<Cluster>("/api/v1/clusters", {
    token,
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function testCluster(
  token: string | null,
  body: { provider: string; kubeconfig: string },
) {
  return apiFetch<HealthInfo>("/api/v1/clusters/test", {
    token,
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function deleteCluster(token: string | null, id: number | string) {
  return apiFetch<null>(`/api/v1/clusters/${id}`, { token, method: "DELETE" });
}

export async function listWorkloads(
  token: string | null,
  clusterId: number | string,
  ns?: string,
) {
  return apiFetch<WorkloadInfo[]>(
    `/api/v1/clusters/${clusterId}/workloads?ns=${ns || "default"}`,
    { token },
  );
}

export async function listEvents(
  token: string | null,
  clusterId: number | string,
  ns?: string,
  limit?: number,
) {
  const q = new URLSearchParams();
  if (ns) q.set("ns", ns);
  if (limit) q.set("limit", String(limit));
  return apiFetch<EventInfo[]>(
    `/api/v1/clusters/${clusterId}/events?${q.toString()}`,
    { token },
  );
}

export async function getDeploymentHistory(
  token: string | null,
  clusterId: number | string,
  name: string,
  ns?: string,
) {
  const q = new URLSearchParams();
  if (ns) q.set("ns", ns);
  return apiFetch<RevisionInfo[]>(
    `/api/v1/clusters/${clusterId}/deployments/${name}/history?${q.toString()}`,
    { token },
  );
}
