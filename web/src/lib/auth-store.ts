"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";
import { apiFetch, ApiException } from "@/lib/api";

export interface User {
  id: number;
  username: string;
  email: string;
  display_name?: string;
}

interface TokenPair {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_at: string;
  user: User;
}

interface MeResponse {
  user: User;
  roles: string[];
  orgs: number[];
  jti: string;
}

interface AuthState {
  user: User | null;
  accessToken: string | null;
  refreshToken: string | null;
  expiresAt: string | null;
  roles: string[];
  orgs: number[];
  /** 是否已经从持久化恢复 */
  hydrated: boolean;

  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<boolean>;
  fetchMe: () => Promise<void>;
  /** 内部:hydrate 完成后调用 */
  setHydrated: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      user: null,
      accessToken: null,
      refreshToken: null,
      expiresAt: null,
      roles: [],
      orgs: [],
      hydrated: false,

      login: async (username, password) => {
        const pair = await apiFetch<TokenPair>("/api/v1/auth/login", {
          method: "POST",
          body: JSON.stringify({ username, password }),
        });
        set({
          user: pair.user,
          accessToken: pair.access_token,
          refreshToken: pair.refresh_token,
          expiresAt: pair.expires_at,
        });
        // 立刻拉 /me 获取 roles + orgs
        await get().fetchMe();
      },

      logout: async () => {
        const { accessToken, refreshToken } = get();
        if (accessToken) {
          try {
            await apiFetch("/api/v1/auth/logout", {
              method: "POST",
              token: accessToken,
              body: JSON.stringify({ refresh_token: refreshToken }),
            });
          } catch {
            /* 即便后端报错也清本地 */
          }
        }
        set({
          user: null, accessToken: null, refreshToken: null,
          expiresAt: null, roles: [], orgs: [],
        });
      },

      refresh: async () => {
        const { refreshToken } = get();
        if (!refreshToken) return false;
        try {
          const pair = await apiFetch<TokenPair>("/api/v1/auth/refresh", {
            method: "POST",
            body: JSON.stringify({ refresh_token: refreshToken }),
          });
          set({
            accessToken: pair.access_token,
            refreshToken: pair.refresh_token,
            expiresAt: pair.expires_at,
            user: pair.user,
          });
          return true;
        } catch (e) {
          if (e instanceof ApiException && e.status === 401) {
            // refresh 失效,清状态
            set({
              user: null, accessToken: null, refreshToken: null,
              expiresAt: null, roles: [], orgs: [],
            });
          }
          return false;
        }
      },

      fetchMe: async () => {
        const { accessToken } = get();
        if (!accessToken) return;
        try {
          const me = await apiFetch<MeResponse>("/api/v1/auth/me", { token: accessToken });
          set({ user: me.user, roles: me.roles, orgs: me.orgs });
        } catch (e) {
          if (e instanceof ApiException && e.status === 401) {
            // 试试 refresh,失败则清
            const ok = await get().refresh();
            if (ok) await get().fetchMe();
          }
        }
      },

      setHydrated: () => set({ hydrated: true }),
    }),
    {
      name: "helios-auth",
      // 只持久化 token + user,不持久化 hydrated
      partialize: (s) => ({
        user: s.user,
        accessToken: s.accessToken,
        refreshToken: s.refreshToken,
        expiresAt: s.expiresAt,
        roles: s.roles,
        orgs: s.orgs,
      }),
      onRehydrateStorage: () => (state) => {
        state?.setHydrated();
      },
    },
  ),
);
