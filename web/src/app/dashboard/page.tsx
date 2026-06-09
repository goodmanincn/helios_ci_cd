"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";

export default function DashboardPage() {
  return (
    <AuthGuard require={true}>
      <DashboardInner />
    </AuthGuard>
  );
}

function DashboardInner() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const roles = useAuthStore((s) => s.roles);
  const orgs = useAuthStore((s) => s.orgs);
  const fetchMe = useAuthStore((s) => s.fetchMe);
  const logout = useAuthStore((s) => s.logout);
  const [menuOpen, setMenuOpen] = useState(false);

  useEffect(() => {
    // 刷新页面后从 /me 拿最新 roles/orgs
    fetchMe();
  }, [fetchMe]);

  async function onLogout() {
    await logout();
    router.replace("/login");
  }

  return (
    <div className="min-h-screen flex">
      {/* Sidebar */}
      <aside
        className="w-60 flex flex-col"
        style={{ background: "var(--bg-elev)", borderRight: "1px solid var(--border)" }}
      >
        <div className="px-5 py-4 flex items-center gap-2" style={{ borderBottom: "1px solid var(--border)" }}>
          <div
            className="w-7 h-7 rounded-md flex items-center justify-center text-sm font-bold"
            style={{ background: "var(--accent)", color: "white" }}
          >
            H
          </div>
          <span className="font-semibold tracking-tight">Helios</span>
        </div>

        <nav className="flex-1 py-3 flex flex-col gap-0.5 px-2 text-sm">
          <NavItem label="仪表盘" icon="◐" active />
          <NavItem label="项目" icon="◇" />
          <NavItem label="流水线" icon="∿" />
          <NavItem label="集群" icon="◆" />
          <NavItem label="主机" icon="□" />
          <NavItem label="密钥" icon="○" />
          <NavItem label="审计" icon="▢" />
        </nav>

        <div className="px-2 py-3" style={{ borderTop: "1px solid var(--border)" }}>
          <button
            className="w-full flex items-center gap-2 px-3 py-2 rounded text-sm"
            style={{ color: "var(--fg-mute)" }}
            onClick={() => setMenuOpen((v) => !v)}
          >
            <div
              className="w-6 h-6 rounded-full flex items-center justify-center text-xs"
              style={{ background: "var(--accent-soft)", color: "var(--accent)" }}
            >
              {user?.username[0]?.toUpperCase() ?? "?"}
            </div>
            <span className="flex-1 text-left">{user?.username ?? "anon"}</span>
            <span style={{ color: "var(--fg-dim)" }}>⌄</span>
          </button>
          {menuOpen && (
            <div
              className="mt-1 rounded-md text-sm overflow-hidden"
              style={{ background: "var(--bg-elev-2)", border: "1px solid var(--border)" }}
            >
              <div className="px-3 py-2" style={{ color: "var(--fg-dim)", borderBottom: "1px solid var(--border)" }}>
                {user?.email}
              </div>
              <button
                className="w-full text-left px-3 py-2 hover:bg-black/20"
                style={{ color: "var(--danger)" }}
                onClick={onLogout}
              >
                登出
              </button>
            </div>
          )}
        </div>
      </aside>

      {/* Main */}
      <main className="flex-1 flex flex-col">
        <header
          className="h-14 px-6 flex items-center justify-between"
          style={{ borderBottom: "1px solid var(--border)" }}
        >
          <h1 className="text-base font-medium">仪表盘</h1>
          <div className="text-xs" style={{ color: "var(--fg-dim)" }}>
            roles: {roles.length ? roles.join(", ") : "—"} · orgs: {orgs.length ? orgs.join(", ") : "—"}
          </div>
        </header>

        <div className="flex-1 px-6 py-8 max-w-5xl mx-auto w-full">
          <div className="card">
            <h2 className="text-lg font-semibold mb-1">
              {greeting()},{user?.display_name || user?.username} 👋
            </h2>
            <p className="text-sm mb-6" style={{ color: "var(--fg-mute)" }}>
              欢迎来到 Helios。当前没有项目,从导入第一个开始吧。
            </p>

            <div
              className="rounded-md flex flex-col items-center justify-center py-12"
              style={{ background: "var(--bg-elev-2)", border: "1px dashed var(--border-strong)" }}
            >
              <div className="text-3xl mb-3" style={{ color: "var(--fg-dim)" }}>◇</div>
              <p className="text-sm mb-4" style={{ color: "var(--fg-mute)" }}>
                还没有项目
              </p>
              <button className="btn btn-primary" disabled title="M1 实现">
                导入项目
              </button>
              <p className="text-xs mt-3" style={{ color: "var(--fg-dim)" }}>
                M1 阶段开放
              </p>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}

function NavItem({ label, icon, active }: { label: string; icon: string; active?: boolean }) {
  return (
    <a
      className="flex items-center gap-2 px-3 py-1.5 rounded text-sm cursor-pointer"
      style={{
        background: active ? "var(--accent-soft)" : "transparent",
        color: active ? "var(--accent)" : "var(--fg-mute)",
      }}
    >
      <span className="w-4 inline-block text-center">{icon}</span>
      <span>{label}</span>
    </a>
  );
}

function greeting(): string {
  const h = new Date().getHours();
  if (h < 6) return "夜深了";
  if (h < 12) return "早上好";
  if (h < 18) return "下午好";
  return "晚上好";
}
