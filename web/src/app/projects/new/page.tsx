"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { AuthGuard } from "@/components/auth-guard";
import { AppShell } from "@/components/app-shell";
import { createProject, RepoType, Visibility } from "@/lib/projects-api";
import { ApiException } from "@/lib/api";

export default function NewProjectPage() {
  return (
    <AuthGuard require={true}>
      <NewProjectInner />
    </AuthGuard>
  );
}

type FormErrors = Partial<Record<"name" | "slug" | "repo_url" | "form", string>>;

function NewProjectInner() {
  const router = useRouter();
  const accessToken = useAuthStore((s) => s.accessToken);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);
  const [description, setDescription] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const [repoType, setRepoType] = useState<RepoType>("github");
  const [defaultBranch, setDefaultBranch] = useState("main");
  const [visibility, setVisibility] = useState<Visibility>("private");
  const [submitting, setSubmitting] = useState(false);
  const [errors, setErrors] = useState<FormErrors>({});

  // 自动从 name 生成 slug,但用户手动编辑 slug 后停止
  function onNameChange(v: string) {
    setName(v);
    if (!slugTouched) {
      setSlug(slugify(v));
    }
  }

  function validate(): FormErrors {
    const e: FormErrors = {};
    if (!name.trim()) e.name = "请填项目名称";
    if (!slug.trim()) e.slug = "请填 slug";
    else if (!/^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$/.test(slug))
      e.slug = "2-64 字符,只允许小写字母/数字/连字符,首末不能是 -";
    if (!repoUrl.trim()) e.repo_url = "请填仓库 URL";
    else if (
      !/^https?:\/\//.test(repoUrl) &&
      !repoUrl.startsWith("git@")
    )
      e.repo_url = "URL 必须以 http(s):// 开头,或 git@host:owner/repo.git 格式";
    return e;
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!accessToken) return;
    const eobj = validate();
    setErrors(eobj);
    if (Object.keys(eobj).length > 0) return;

    setSubmitting(true);
    try {
      const p = await createProject(accessToken, {
        name: name.trim(),
        slug: slug.trim(),
        description: description.trim() || undefined,
        repo_url: repoUrl.trim(),
        repo_type: repoType,
        default_branch: defaultBranch.trim() || undefined,
        visibility,
      });
      router.replace(`/projects/${p.id}`);
    } catch (e) {
      if (e instanceof ApiException) {
        if (e.status === 409) {
          setErrors({ slug: "slug 已被占用,换一个" });
        } else if (e.status === 400) {
          setErrors({ form: e.message });
        } else {
          setErrors({ form: e.message });
        }
      } else {
        setErrors({ form: String(e) });
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AppShell title="新建项目">
      <div className="px-6 py-8 max-w-2xl mx-auto w-full">
        <div className="mb-4 text-sm" style={{ color: "var(--fg-dim)" }}>
          <Link href="/projects" className="hover:underline">项目</Link>
          <span className="mx-2">/</span>
          <span style={{ color: "var(--fg)" }}>新建</span>
        </div>

        <form onSubmit={onSubmit} className="card flex flex-col gap-4">
          <Field label="项目名称" required error={errors.name}>
            <input
              className="input"
              value={name}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder="API Gateway"
              autoFocus
            />
          </Field>

          <Field
            label="Slug"
            required
            error={errors.slug}
            hint="URL 友好的唯一标识(项目级唯一),只允许小写字母/数字/连字符"
          >
            <input
              className="input font-mono text-sm"
              value={slug}
              onChange={(e) => {
                setSlug(e.target.value);
                setSlugTouched(true);
              }}
              placeholder="api-gateway"
            />
          </Field>

          <Field label="描述">
            <textarea
              className="input min-h-20 resize-y"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="项目简介,可选"
            />
          </Field>

          <div className="grid grid-cols-3 gap-3">
            <Field label="仓库类型" required>
              <select
                className="input"
                value={repoType}
                onChange={(e) => setRepoType(e.target.value as RepoType)}
              >
                <option value="github">GitHub</option>
                <option value="gitlab">GitLab</option>
                <option value="gitee">Gitee</option>
                <option value="bitbucket">Bitbucket</option>
              </select>
            </Field>
            <Field label="默认分支">
              <input
                className="input"
                value={defaultBranch}
                onChange={(e) => setDefaultBranch(e.target.value)}
                placeholder="main"
              />
            </Field>
            <Field label="可见性">
              <select
                className="input"
                value={visibility}
                onChange={(e) => setVisibility(e.target.value as Visibility)}
              >
                <option value="private">私有</option>
                <option value="public">公开</option>
              </select>
            </Field>
          </div>

          <Field label="仓库 URL" required error={errors.repo_url}>
            <input
              className="input font-mono text-sm"
              value={repoUrl}
              onChange={(e) => setRepoUrl(e.target.value)}
              placeholder="https://github.com/acme/api.git"
            />
          </Field>

          {errors.form && <div className="err-msg">{errors.form}</div>}

          <div className="flex items-center justify-end gap-2 pt-2">
            <Link href="/projects" className="btn">取消</Link>
            <button
              type="submit"
              className="btn btn-primary"
              disabled={submitting}
            >
              {submitting ? "创建中..." : "创建项目"}
            </button>
          </div>
        </form>
      </div>
    </AppShell>
  );
}

function Field({
  label,
  required,
  error,
  hint,
  children,
}: {
  label: string;
  required?: boolean;
  error?: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="label">
        {label}
        {required && <span style={{ color: "var(--danger)" }}> *</span>}
      </label>
      {children}
      {hint && !error && (
        <div className="text-xs" style={{ color: "var(--fg-dim)" }}>{hint}</div>
      )}
      {error && (
        <div className="text-xs" style={{ color: "var(--danger)" }}>{error}</div>
      )}
    </div>
  );
}

function slugify(s: string): string {
  return s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
}
