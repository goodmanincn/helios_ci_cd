// API base URL — 通过 NEXT_PUBLIC_API_BASE 覆盖,默认 dev 后端
export const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE || "http://127.0.0.1:18080";

export interface ApiError {
  error: string;
  message: string;
}

export class ApiException extends Error {
  status: number;
  payload: ApiError;
  constructor(status: number, payload: ApiError) {
    super(payload.message || payload.error);
    this.status = status;
    this.payload = payload;
  }
}

/**
 * 通用 fetch 包装。
 * - 自动加 Content-Type
 * - 如果提供 token,自动加 Authorization
 * - 4xx/5xx 抛 ApiException
 */
export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit & { token?: string | null } = {},
): Promise<T> {
  const { token, headers, ...rest } = init;
  const h = new Headers(headers || {});
  h.set("Content-Type", "application/json");
  if (token) h.set("Authorization", `Bearer ${token}`);

  const res = await fetch(API_BASE + path, { ...rest, headers: h });
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    throw new ApiException(res.status, (data as ApiError) || { error: "unknown", message: res.statusText });
  }
  return data as T;
}
