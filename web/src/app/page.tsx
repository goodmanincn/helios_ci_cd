"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";

/** 根路由 — 按登录状态分流到 /login 或 /dashboard。 */
export default function Home() {
  const router = useRouter();
  const hydrated = useAuthStore((s) => s.hydrated);
  const hasUser = useAuthStore((s) => !!s.user && !!s.accessToken);

  useEffect(() => {
    if (!hydrated) return;
    router.replace(hasUser ? "/dashboard" : "/login");
  }, [hydrated, hasUser, router]);

  return (
    <div className="min-h-screen flex items-center justify-center" style={{ color: "var(--fg-dim)" }}>
      正在加载…
    </div>
  );
}
