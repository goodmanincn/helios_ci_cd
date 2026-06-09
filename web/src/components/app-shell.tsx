"use client";

import { ReactNode, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";

interface NavSpec {
  href: string;
  label: string;
  icon: string;
  enabled: boolean;
}

const NAV: NavSpec[] = [
  { href: "/dashboard", label: "仪表盘", icon: "◐", enabled: true },
  { href: "/projects", label: "项目", icon: "◇", enabled: true },
  { href: "/pipelines", label: "流水线", icon: "∿", enabled: false },
  { href: "/clusters", label: "集群", icon: "◆", enabled: false },
  { href: "/hosts", label: "主机", icon: "□", enabled: false },
  { href: "/secrets", label: "密钥", icon: "○", enabled: false },
  { href: "/audit", label: "审计", icon: "▢", enabled: false },
];

interface AppShellProps {
  title: string;
  actions?: ReactNode;
  children: ReactNode;
}

export function AppShell({ title, actions, children }: AppShellProps) {
  const pathname = usePathname();
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const roles = useAuthStore((s) => s.roles);
  const orgs = useAuthStore((s) => s.orgs);
  const logout = useAuthStore((s) => s.logout);
  const [menuOpen, setMenuOpen] = useState(false);

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
        <div
          className="px-5 py-4 flex items-center gap-2"
          style={{ borderBottom: "1px solid var(--border)" }}
        >
          <div
            className="w-7 h-7 rounded-md flex items-center justify-center text-sm font-bold"
            style={{ background: "var(--accent)", color: "white" }}
          >
            H
          </div>
          <span className="font-semibold tracking-tight">Helios</span>
        </div>

        <nav className="flex-1 py-3 flex flex-col gap-0.5 px-2 text-sm">
          {NAV.map((n) => {
            const active = pathname === n.href || pathname.startsWith(n.href + "/");
            const cls = "flex items-center gap-2 px-3 py-1.5 rounded text-sm";
            const style = {
              background: active ? "var(--accent-soft)" : "transparent",
              color: active
                ? "var(--accent)"
                : n.enabled
                  ? "var(--fg-mute)"
                  : "var(--fg-dim)",
              cursor: n.enabled ? "pointer" : "not-allowed",
            } as const;
            const inner = (
              <>
                <span className="w-4 inline-block text-center">{n.icon}</span>
                <span>{n.label}</span>
                {!n.enabled && (
                  <span
                    className="ml-auto text-[10px] px-1 rounded"
                    style={{ background: "var(--bg-elev-2)", color: "var(--fg-dim)" }}
                  >
                    soon
                  </span>
                )}
              </>
            );
            return n.enabled ? (
              <Link key={n.href} href={n.href} className={cls} style={style}>
                {inner}
              </Link>
            ) : (
              <span key={n.href} className={cls} style={style} title="后续 milestone 开放">
                {inner}
              </span>
            );
          })}
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
              <div
                className="px-3 py-2"
                style={{ color: "var(--fg-dim)", borderBottom: "1px solid var(--border)" }}
              >
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
          <h1 className="text-base font-medium">{title}</h1>
          <div className="flex items-center gap-4">
            {actions}
            <div className="text-xs" style={{ color: "var(--fg-dim)" }}>
              roles: {roles.length ? roles.join(", ") : "—"} · orgs: {orgs.length ? orgs.join(", ") : "—"}
            </div>
          </div>
        </header>

        <div className="flex-1 overflow-auto">{children}</div>
      </main>
    </div>
  );
}
