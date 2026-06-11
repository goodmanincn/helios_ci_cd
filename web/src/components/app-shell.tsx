"use client";

import { ReactNode, useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { apiFetch } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";

// T1.7.1/T1.7.2: 重做 sidebar + header, 对齐 ui/projects.html 的分组样式。
// 与原型差异: 用 next/link 走 SPA 路由, 暂停留 emoji 图标 (原型一致), 计数走 /api/v1/me/counters。
//
// 仅启用的菜单: 项目 / 执行记录 / 仪表盘 (已有路由)。
// 其它项 (流水线/集群/主机/密钥/插件/模板/最近活动/帮助) 走 disabled 灰显, hover 不响应路由。

interface NavItem {
  href: string;
  label: string;
  icon: string;
  enabled: boolean;
  badgeKey?: "projects" | "runs"; // 对应 counters 字段
}

interface NavGroup {
  title: string;
  items: NavItem[];
}

const NAV_GROUPS: NavGroup[] = [
  {
    title: "工作区",
    items: [
      { href: "/projects", label: "项目", icon: "📦", enabled: true, badgeKey: "projects" },
      { href: "/pipelines", label: "流水线", icon: "🔀", enabled: true },
      { href: "/runs", label: "执行记录", icon: "▶", enabled: true, badgeKey: "runs" },
      { href: "/dashboard", label: "仪表盘", icon: "◐", enabled: true },
    ],
  },
  {
    title: "基础设施",
    items: [
      { href: "/clusters", label: "集群", icon: "☸", enabled: true },
      { href: "/hosts", label: "主机", icon: "🖥", enabled: true },
      { href: "/secrets", label: "密钥", icon: "🔐", enabled: true },
    ],
  },
  {
    title: "市场",
    items: [
      { href: "/plugins", label: "插件", icon: "🧩", enabled: true },
      { href: "/templates", label: "模板", icon: "📋", enabled: true },
    ],
  },
];

interface Counters {
  projects: number;
  runs: number;
}

interface AppShellProps {
  /** 面包屑当前页文字 (header 显示, 不传则只显示 workspace) */
  title: string;
  /** header 右侧自定义动作槽位 (例如 "+ 新建项目") */
  actions?: ReactNode;
  /** shell-content 额外 className (e.g. 覆盖 overflow) */
  contentClassName?: string;
  children: ReactNode;
}

export function AppShell({ title, actions, contentClassName, children }: AppShellProps) {
  const pathname = usePathname();
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const accessToken = useAuthStore((s) => s.accessToken);
  const logout = useAuthStore((s) => s.logout);

  const [menuOpen, setMenuOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [counters, setCounters] = useState<Counters>({ projects: 0, runs: 0 });

  // 拉计数 (徽章用) — 失败静默, 留 0
  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;
    apiFetch<Counters>("/api/v1/me/counters", { token: accessToken })
      .then((c) => {
        if (!cancelled) setCounters(c);
      })
      .catch(() => {
        // 静默 — 接口可能未部署或返错, sidebar 应优雅降级
      });
    return () => {
      cancelled = true;
    };
  }, [accessToken, pathname]);

  // ⌘K 快捷键
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setSearchOpen(true);
      } else if (e.key === "Escape") {
        setSearchOpen(false);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  async function onLogout() {
    await logout();
    router.replace("/login");
  }

  const workspaceName = "Acme Inc."; // M1: 单 workspace 占位

  return (
    <div className="min-h-screen flex">
      {/* ===== Sidebar ===== */}
      <aside className="shell-sidebar">
        <div className="logo-row">
          <div className="logo-mark" />
          <span className="logo-text">Helios</span>
        </div>

        <button className="org-switcher" type="button" title="切换 workspace (M2)">
          <div className="org-avatar">{workspaceName[0]}</div>
          <span className="org-name">{workspaceName}</span>
          <span className="org-caret">⌄</span>
        </button>

        <nav className="nav-groups">
          {NAV_GROUPS.map((g) => (
            <div className="nav-group" key={g.title}>
              <div className="nav-title">{g.title}</div>
              {g.items.map((n) => {
                const active =
                  n.enabled && (pathname === n.href || pathname.startsWith(n.href + "/"));
                const badge =
                  n.badgeKey && counters[n.badgeKey] > 0 ? counters[n.badgeKey] : null;
                const content = (
                  <>
                    <span className="nav-icon">{n.icon}</span>
                    <span className="nav-label">{n.label}</span>
                    {badge != null && <span className="nav-badge">{badge}</span>}
                    {!n.enabled && <span className="nav-soon">soon</span>}
                  </>
                );
                if (n.enabled) {
                  return (
                    <Link
                      key={n.href}
                      href={n.href}
                      className={`nav-item ${active ? "active" : ""}`}
                    >
                      {content}
                    </Link>
                  );
                }
                return (
                  <span
                    key={n.href}
                    className="nav-item disabled"
                    title="后续 milestone 开放"
                  >
                    {content}
                  </span>
                );
              })}
            </div>
          ))}
        </nav>

        <div className="sidebar-footer">
          <span className="nav-item disabled">
            <span className="nav-icon">🕐</span>
            <span className="nav-label">最近活动</span>
          </span>
          <span className="nav-item disabled">
            <span className="nav-icon">❓</span>
            <span className="nav-label">帮助与文档</span>
          </span>

          <button
            type="button"
            className="user-row"
            onClick={() => setMenuOpen((v) => !v)}
          >
            <div className="user-avatar">{user?.username[0]?.toUpperCase() ?? "?"}</div>
            <span className="user-name">{user?.username ?? "anon"}</span>
            <span className="user-caret">⌄</span>
          </button>
          {menuOpen && (
            <div className="user-menu">
              <div className="user-menu-email">{user?.email}</div>
              <button className="user-menu-logout" onClick={onLogout}>
                登出
              </button>
            </div>
          )}
        </div>
      </aside>

      {/* ===== Main ===== */}
      <main className="shell-main">
        <header className="shell-header">
          <div className="breadcrumb">
            <span className="bc-org">{workspaceName}</span>
            <span className="bc-sep">/</span>
            <span className="bc-current">{title}</span>
          </div>
          <div className="header-actions">
            <button
              type="button"
              className="search-pill"
              onClick={() => setSearchOpen(true)}
            >
              <span className="search-text">搜索 / 命令</span>
              <span className="search-kbd">⌘K</span>
            </button>
            <button
              type="button"
              className="header-ghost"
              title="通知 (M2)"
              aria-label="通知"
            >
              🔔
            </button>
            {actions}
          </div>
        </header>

        <div className={`shell-content ${contentClassName || ""}`}>{children}</div>
      </main>

      {/* ===== ⌘K 命令面板占位 ===== */}
      {searchOpen && (
        <div className="cmdk-overlay" onClick={() => setSearchOpen(false)}>
          <div className="cmdk-panel" onClick={(e) => e.stopPropagation()}>
            <input
              type="text"
              className="cmdk-input"
              placeholder="搜索项目 / 跳转命令... (M2 接入)"
              autoFocus
            />
            <div className="cmdk-hint">
              命令面板将在 M2 接入。Esc 关闭。
            </div>
          </div>
        </div>
      )}

      <style jsx>{`
        .shell-sidebar {
          width: 240px;
          background: var(--bg-elev);
          border-right: 1px solid var(--border);
          padding: 20px 14px;
          display: flex;
          flex-direction: column;
          flex-shrink: 0;
        }
        .logo-row {
          display: flex;
          align-items: center;
          gap: 8px;
          padding: 4px 8px 18px;
          font-weight: 590;
          font-size: 15px;
          letter-spacing: -0.24px;
        }
        .logo-mark {
          width: 20px;
          height: 20px;
          background: linear-gradient(135deg, #5e6ad2, #7170ff);
          border-radius: 5px;
        }
        .logo-text {
          color: var(--fg);
        }
        .org-switcher {
          display: flex;
          align-items: center;
          gap: 8px;
          padding: 8px;
          background: rgba(255, 255, 255, 0.02);
          border: 1px solid var(--border);
          border-radius: 6px;
          margin-bottom: 16px;
          font-size: 13px;
          font-weight: 510;
          color: var(--fg-mute);
          cursor: pointer;
          width: 100%;
          text-align: left;
        }
        .org-switcher:hover {
          background: rgba(255, 255, 255, 0.05);
          color: var(--fg);
        }
        .org-avatar {
          width: 18px;
          height: 18px;
          border-radius: 4px;
          background: #d97706;
          color: white;
          display: flex;
          align-items: center;
          justify-content: center;
          font-size: 10px;
          font-weight: 600;
          flex-shrink: 0;
        }
        .org-name {
          flex: 1;
        }
        .org-caret {
          color: var(--fg-dim);
          font-size: 12px;
        }
        .nav-groups {
          flex: 1;
          overflow-y: auto;
        }
        .nav-group {
          margin-bottom: 18px;
        }
        .nav-title {
          font-size: 11px;
          font-weight: 510;
          color: var(--fg-dim);
          text-transform: uppercase;
          letter-spacing: 0.4px;
          padding: 0 8px 6px;
        }
        :global(.nav-item) {
          display: flex;
          align-items: center;
          gap: 10px;
          padding: 6px 8px;
          font-size: 13px;
          font-weight: 510;
          color: var(--fg-mute);
          border-radius: 5px;
          cursor: pointer;
          text-decoration: none;
          transition: background 0.15s, color 0.15s;
        }
        :global(.nav-item:hover) {
          background: rgba(255, 255, 255, 0.03);
          color: var(--fg);
        }
        :global(.nav-item.active) {
          background: var(--accent-soft);
          color: var(--fg);
        }
        :global(.nav-item.disabled) {
          color: var(--fg-dim);
          cursor: not-allowed;
        }
        :global(.nav-item.disabled:hover) {
          background: transparent;
          color: var(--fg-dim);
        }
        :global(.nav-icon) {
          width: 16px;
          display: inline-flex;
          justify-content: center;
          font-size: 13px;
        }
        :global(.nav-label) {
          flex: 1;
        }
        :global(.nav-badge) {
          margin-left: auto;
          font-size: 10px;
          padding: 1px 6px;
          background: rgba(255, 255, 255, 0.06);
          border-radius: 3px;
          color: var(--fg-mute);
          font-weight: 600;
        }
        :global(.nav-soon) {
          margin-left: auto;
          font-size: 10px;
          padding: 1px 5px;
          background: var(--bg-elev-2);
          border-radius: 3px;
          color: var(--fg-dim);
        }
        .sidebar-footer {
          margin-top: 12px;
          padding-top: 12px;
          border-top: 1px solid var(--border);
          display: flex;
          flex-direction: column;
          gap: 2px;
        }
        .user-row {
          margin-top: 10px;
          display: flex;
          align-items: center;
          gap: 8px;
          padding: 6px 8px;
          border-radius: 5px;
          cursor: pointer;
          background: transparent;
          border: none;
          color: var(--fg-mute);
          font-size: 13px;
          font-weight: 510;
          text-align: left;
          width: 100%;
        }
        .user-row:hover {
          background: rgba(255, 255, 255, 0.03);
        }
        .user-avatar {
          width: 22px;
          height: 22px;
          border-radius: 5px;
          background: var(--accent-soft);
          color: var(--accent);
          display: flex;
          align-items: center;
          justify-content: center;
          font-size: 11px;
          font-weight: 600;
        }
        .user-name {
          flex: 1;
        }
        .user-caret {
          color: var(--fg-dim);
          font-size: 12px;
        }
        .user-menu {
          margin-top: 4px;
          border: 1px solid var(--border);
          border-radius: 6px;
          background: var(--bg-elev-2);
          overflow: hidden;
        }
        .user-menu-email {
          padding: 8px 12px;
          color: var(--fg-dim);
          font-size: 12px;
          border-bottom: 1px solid var(--border);
        }
        .user-menu-logout {
          display: block;
          width: 100%;
          padding: 8px 12px;
          background: transparent;
          border: none;
          color: var(--danger);
          font-size: 13px;
          text-align: left;
          cursor: pointer;
        }
        .user-menu-logout:hover {
          background: rgba(0, 0, 0, 0.2);
        }
        .shell-main {
          flex: 1;
          display: flex;
          flex-direction: column;
          min-width: 0;
          background: var(--bg);
        }
        .shell-header {
          height: 52px;
          border-bottom: 1px solid var(--border);
          background: var(--bg);
          display: flex;
          align-items: center;
          padding: 0 24px;
          gap: 16px;
        }
        .breadcrumb {
          display: flex;
          align-items: center;
          gap: 8px;
          font-size: 13px;
          font-weight: 510;
          color: var(--fg-mute);
        }
        .bc-sep {
          color: var(--fg-dim);
        }
        .bc-current {
          color: var(--fg);
        }
        .header-actions {
          margin-left: auto;
          display: flex;
          align-items: center;
          gap: 8px;
        }
        .search-pill {
          display: flex;
          align-items: center;
          gap: 8px;
          padding: 5px 10px;
          background: rgba(255, 255, 255, 0.02);
          border: 1px solid var(--border);
          border-radius: 5px;
          font-size: 12px;
          color: var(--fg-mute);
          min-width: 220px;
          cursor: pointer;
        }
        .search-pill:hover {
          background: rgba(255, 255, 255, 0.05);
        }
        .search-text {
          color: var(--fg-dim);
        }
        .search-kbd {
          margin-left: auto;
          padding: 1px 5px;
          font-size: 10px;
          background: rgba(255, 255, 255, 0.05);
          border: 1px solid var(--border);
          border-radius: 3px;
          color: var(--fg-dim);
          font-family: ui-monospace, Menlo, monospace;
        }
        .header-ghost {
          background: transparent;
          border: none;
          color: var(--fg-mute);
          cursor: pointer;
          padding: 4px 8px;
          border-radius: 4px;
          font-size: 14px;
        }
        .header-ghost:hover {
          background: rgba(255, 255, 255, 0.04);
          color: var(--fg);
        }
        .shell-content {
          flex: 1;
          overflow-y: auto;
        }
        .cmdk-overlay {
          position: fixed;
          inset: 0;
          background: rgba(0, 0, 0, 0.6);
          display: flex;
          justify-content: center;
          align-items: flex-start;
          padding-top: 120px;
          z-index: 100;
        }
        .cmdk-panel {
          width: 520px;
          max-width: 90vw;
          background: var(--bg-elev);
          border: 1px solid var(--border);
          border-radius: 8px;
          box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
          overflow: hidden;
        }
        .cmdk-input {
          width: 100%;
          padding: 14px 16px;
          background: transparent;
          color: var(--fg);
          font-size: 14px;
          border: none;
          outline: none;
          border-bottom: 1px solid var(--border);
        }
        .cmdk-input::placeholder {
          color: var(--fg-dim);
        }
        .cmdk-hint {
          padding: 12px 16px;
          font-size: 12px;
          color: var(--fg-dim);
        }
      `}</style>
    </div>
  );
}
