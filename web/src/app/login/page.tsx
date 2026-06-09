"use client";

import { useState, FormEvent } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { ApiException } from "@/lib/api";

export default function LoginPage() {
  return (
    <AuthGuard require={false}>
      <LoginInner />
    </AuthGuard>
  );
}

function LoginInner() {
  const router = useRouter();
  const login = useAuthStore((s) => s.login);
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);
    setLoading(true);
    try {
      await login(username, password);
      router.replace("/dashboard");
    } catch (e) {
      if (e instanceof ApiException) {
        setErr(e.payload.message || e.payload.error);
      } else {
        setErr(String(e));
      }
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <div className="flex flex-col items-center mb-8">
          <div
            className="w-12 h-12 rounded-lg mb-3 flex items-center justify-center font-bold text-lg"
            style={{ background: "var(--accent)", color: "white" }}
          >
            H
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Helios</h1>
          <p className="text-sm mt-1" style={{ color: "var(--fg-mute)" }}>
            多云原生 CI/CD 平台
          </p>
        </div>

        <form onSubmit={onSubmit} className="card flex flex-col gap-4">
          {err && <div className="err-msg">{err}</div>}

          <div>
            <label className="label" htmlFor="username">用户名</label>
            <input
              id="username"
              className="input"
              autoFocus
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
            />
          </div>

          <div>
            <label className="label" htmlFor="password">密码</label>
            <input
              id="password"
              type="password"
              className="input"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>

          <button type="submit" className="btn btn-primary mt-2" disabled={loading}>
            {loading ? "登录中…" : "登录"}
          </button>

          <p className="text-xs text-center" style={{ color: "var(--fg-dim)" }}>
            开发默认账户: admin / admin12345
          </p>
        </form>
      </div>
    </div>
  );
}
