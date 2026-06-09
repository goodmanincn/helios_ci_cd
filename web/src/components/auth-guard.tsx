"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";

/**
 * 路由守卫组件:
 * - require=true 要求登录,否则跳 /login
 * - require=false 要求未登录,否则跳 /dashboard (用于 /login 页)
 *
 * 等 hydrated 完成后再判断,避免 SSR/hydrate 闪烁。
 */
export function AuthGuard({
  require,
  children,
}: {
  require: boolean;
  children: React.ReactNode;
}) {
  const router = useRouter();
  const hydrated = useAuthStore((s) => s.hydrated);
  const hasUser = useAuthStore((s) => !!s.user && !!s.accessToken);

  useEffect(() => {
    if (!hydrated) return;
    if (require && !hasUser) router.replace("/login");
    if (!require && hasUser) router.replace("/dashboard");
  }, [hydrated, hasUser, require, router]);

  if (!hydrated) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div style={{ color: "var(--fg-dim)" }}>加载中…</div>
      </div>
    );
  }
  // 守卫不通过时,渲染 null 避免闪现内容
  if (require && !hasUser) return null;
  if (!require && hasUser) return null;
  return <>{children}</>;
}
