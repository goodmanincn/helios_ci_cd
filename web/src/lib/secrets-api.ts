import { apiFetch } from "./api";

export type SecretType =
  | "text"
  | "file"
  | "kubeconfig"
  | "ssh-key"
  | "cloud-credential"
  | "tencent_cloud"
  | "aliyun_cloud";

export interface SecretRef {
  kind: "cluster" | "host";
  id: number;
  name: string;
}

export interface Secret {
  id: number;
  scope: string;
  scope_id: number;
  name: string;
  type: SecretType;
  description?: string;
  kek_id?: string;
  references?: SecretRef[];
  created_by?: number;
  created_at: string;
  updated_at: string;
}

export interface SecretListResp {
  items: Secret[];
  total: number;
}

export interface TencentCloudValue {
  secret_id: string;
  secret_key: string;
  region: string;
  role_arn?: string;
}

export interface AliyunCloudValue {
  access_key_id: string;
  access_key_secret: string;
  region: string;
  role_arn?: string;
}

export async function listSecrets(
  token: string | null,
  opts?: { type?: SecretType; q?: string },
) {
  const q = new URLSearchParams();
  if (opts?.type) q.set("type", opts.type);
  if (opts?.q) q.set("q", opts.q);
  const qs = q.toString();
  return apiFetch<SecretListResp>(`/api/v1/secrets${qs ? `?${qs}` : ""}`, { token });
}

export async function createSecret(
  token: string | null,
  body: {
    scope: string;
    scope_id: number;
    name: string;
    type: SecretType;
    value: string;
    description?: string;
  },
) {
  return apiFetch<Secret>("/api/v1/secrets", {
    token,
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function updateSecret(
  token: string | null,
  id: number,
  body: { value?: string; description?: string },
) {
  return apiFetch<Secret>(`/api/v1/secrets/${id}`, {
    token,
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export async function deleteSecret(token: string | null, id: number) {
  return apiFetch<null>(`/api/v1/secrets/${id}`, { token, method: "DELETE" });
}
