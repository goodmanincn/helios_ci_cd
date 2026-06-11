// 插件市场 API client (M9).
import { apiFetch } from "./api";

export interface Plugin {
  id: number;
  namespace: string;
  name: string;
  slug: string;
  description?: string;
  category?: string;
  publisher?: string;
  repository?: string;
  verified: boolean;
  official: boolean;
  downloads: number;
  latest_version?: string;
  created_at: string;
}

export interface PluginVersion {
  id: number;
  plugin_id: number;
  version: string;
  action_yml: string;
  action_spec: Record<string, unknown>;
  readme?: string;
  changelog?: string;
  is_latest: boolean;
  created_at: string;
}

export interface PluginDetail {
  plugin: Plugin;
  versions: PluginVersion[];
  installed?: boolean;
  installed_version?: string;
}

export interface InstalledPlugin {
  installation: {
    id: number;
    org_id: number;
    plugin_id: number;
    version_id: number;
    installed_by?: number;
    installed_at: string;
  };
  plugin: Plugin;
  version: PluginVersion;
}

export async function listPlugins(
  token: string | null,
  filters?: { q?: string; category?: string; verified?: boolean },
) {
  const qs = new URLSearchParams();
  if (filters?.q) qs.set("q", filters.q);
  if (filters?.category) qs.set("category", filters.category);
  if (filters?.verified) qs.set("verified", "true");
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiFetch<Plugin[]>(`/api/v1/plugins${suffix}`, { token });
}

export async function getPlugin(token: string | null, slug: string) {
  return apiFetch<PluginDetail>(`/api/v1/plugins/${slug}`, { token });
}

export async function installPlugin(token: string | null, slug: string, version?: string) {
  return apiFetch<{ plugin_id: number; version_id: number; version: string; org_id: number }>(
    `/api/v1/plugins/${slug}/install`,
    {
      token,
      method: "POST",
      body: JSON.stringify({ version: version || "latest" }),
    },
  );
}

export async function uninstallPlugin(token: string | null, slug: string) {
  return apiFetch<void>(`/api/v1/plugins/${slug}/install`, {
    token,
    method: "DELETE",
  });
}

export async function listInstalled(token: string | null) {
  return apiFetch<InstalledPlugin[]>("/api/v1/plugins/installed", { token });
}
