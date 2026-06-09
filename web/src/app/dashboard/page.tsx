"use client";

import { useEffect } from "react";
import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { AppShell } from "@/components/app-shell";

export default function DashboardPage() {
  return (
    <AuthGuard require={true}>
      <DashboardInner />
    </AuthGuard>
  );
}

function DashboardInner() {
  const user = useAuthStore((s) => s.user);
  const fetchMe = useAuthStore((s) => s.fetchMe);

  useEffect(() => {
    fetchMe();
  }, [fetchMe]);

  return (
    <AppShell title="仪表盘">
      <div className="px-6 py-8 max-w-5xl mx-auto w-full">
        <div className="card">
          <h2 className="text-lg font-semibold mb-1">
            {greeting()},{user?.display_name || user?.username} 👋
          </h2>
          <p className="text-sm mb-6" style={{ color: "var(--fg-mute)" }}>
            欢迎来到 Helios。去「项目」页面看看,或导入第一个仓库。
          </p>

          <div className="grid gap-4 sm:grid-cols-3">
            <Stat label="项目" value="—" hint="待加载" />
            <Stat label="本月运行" value="—" hint="M1.3 接入" />
            <Stat label="平均时长" value="—" hint="M1.3 接入" />
          </div>
        </div>
      </div>
    </AppShell>
  );
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div
      className="rounded-md p-4"
      style={{ background: "var(--bg-elev-2)", border: "1px solid var(--border)" }}
    >
      <div className="text-xs mb-1" style={{ color: "var(--fg-dim)" }}>{label}</div>
      <div className="text-2xl font-semibold">{value}</div>
      {hint && (
        <div className="text-xs mt-1" style={{ color: "var(--fg-dim)" }}>{hint}</div>
      )}
    </div>
  );
}

function greeting(): string {
  const h = new Date().getHours();
  if (h < 6) return "夜深了";
  if (h < 12) return "早上好";
  if (h < 18) return "下午好";
  return "晚上好";
}
