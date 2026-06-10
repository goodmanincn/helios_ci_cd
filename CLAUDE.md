# Helios CI/CD — Claude Code 项目记忆

本文件从 Hermes Agent 的持久化记忆 (MEMORY.md + USER.md) 沉淀而来,仅保留与本项目相关的部分。
项目特定深度知识见 `~/.claude/skills/` (如果未来添加 `helios-ci-cd-project` skill)。

---

## 沟通约定

- **语言**: 中文沟通,文档用中文
- **风格**: 终端友好的简洁文本,少 markdown 装饰,避免大段 emoji
- **回应深度**: 直接给结果 + 关键决策理由,不复述任务

---

## 工作流硬性约束 ⚠️ 不可违反

**每完成一个 task / 子任务 / 有意义的一轮改动收尾,必须立刻执行:**

```bash
git add -A && git commit -m "中文简述本轮做的事" && git push origin main
```

- 不等用户提醒,不留到 "下一轮一起 commit"
- 代码改完没 commit 就开始做下一件事 = **违规**
- 验证步骤 (`go build` / `go test`) 跑完一轮也算一个收尾点
- commit message 一律中文简述

用户已两次因忘 commit 表达不满。务必严守。

---

## 验证标准

每次有意义的改动后,**两个模块都要跑**:

```bash
cd /Users/jie/Documents/haha/hermes_project/helios_ci_cd
go build ./...
go test ./...
```

- 必须 api + worker 两个 module 都过
- 只在剩余未覆盖行是纯基础设施 / DB error 路径 (无需 mock) 时才能停手
- 集成测试需要 `HELIOS_TEST_DB_DSN` 环境变量,通常从 `.env` 取

---

## 项目架构速查

**Go multi-module monorepo (go.work, Go 1.25.11)**

模块布局:
```
api/      — REST/SSE 后端 + handlers (Gin + GORM)
worker/   — asynq 异步任务执行器 (git checkout / build run)
runner/   — 容器内执行 step 的 sidecar
cli/      — helios CLI
web/      — Next.js + TS 前端 (App Router, dark theme)
```

模块路径前缀: `github.com/helios-cicd/helios/<module>`

**禁跨 module `internal/`** — 共享代码放各自 `pkg/`。

异步任务类型一律以 `helios:` 前缀,例如 `helios:git:checkout`。

---

## 数据库 & 基础设施

- **Postgres**: docker 容器 `helios-postgres`
- **Redis**: 容器 `helios-redis` (容器内 6379 / host 6380)
- **DSN**: 从 `.env` 的 `HELIOS_DB_DSN` / `HELIOS_TEST_DB_DSN` 取

**Hermes 终端脱敏陷阱**:命令含 `SECRET`/`PASSWORD`/`TOKEN` 子串的字面会被 `***` 替换,导致 env 设值失败或 HMAC 算空。
对策:
- 用 `set -a; source .env; set +a` 加载敏感值,不要 inline `SECRET=xxx cmd`
- 必要时把敏感串写入文件,命令读文件而非命令行字面

---

## 测试 fixture 范式

- handler 集成测试用 `withRunTx(t, func(tx *gorm.DB){...})` — 事务里建 fixture,测完回滚
- **runstate.Machine 例外** — 它走独立 `*sql.DB` 看不见 GORM tx,需用 committed fixture + 手工 `t.Cleanup` 倒序删
- seed 用户: `username=admin / password=admin12345` (不是 email!)
- Pipeline 必须有 Slug, Run 必须有 TriggerType,否则 check constraint 炸

---

## E2E 环境变量 (本地跑 api/worker)

```bash
HELIOS_BUILD_RUNTIME=host
HELIOS_LOG_ARCHIVE_DIR=/tmp/helios-e2e/logs
HELIOS_WORKSPACE_DIR=/tmp/helios-e2e/runs
HELIOS_REDIS_ADDR=127.0.0.1:6380
```

启动顺序: postgres + redis 容器 → api (port 8080) → worker → web dev (port 3100)
Web 启动:
```bash
cd web && NEXT_PUBLIC_API_BASE=http://localhost:8080 pnpm exec next dev -p 3100
```

---

## 前端约定

- Stack: Next.js + TypeScript + App Router
- API 客户端: `web/src/lib/api.ts` 的 `apiFetch` + 导出的 `API_BASE`
- **暗色主题** — 用 `--bg #0a0a0f` 等深色 token,**不要按白底配色**
- ESLint 规则: `react-hooks/set-state-in-effect` 等严格规则会拦,精准单行 disable

---

## SSE 日志契约 (T1.6.3)

- 历史: `GET /api/v1/runs/:id/logs?source=auto&count=N`
- 流: `GET /api/v1/runs/:id/logs/stream?token=<jwt>` (EventSource 不能加 header,走 query)
- 事件: `event: log` + `data: {ts,stream,line}`;另有 `event: ping` / `event: end`
- terminal run 不开流,只拉历史快照
- dev 模式 token 为空时放行 (M1 简化, M2 prod 化)

---

## 当前 milestone 状态

M1 进度: E1.1–E1.5 ✅, E1.6 进行中
- T1.6.1 ✅ run 详情/列表 API
- T1.6.2 ✅ 详情页 UI
- T1.6.3 ✅ SSE 实时日志面板 (commit `198f8ca`)
- T1.6.4 🔄 cancel/retry — 后端代码就位,测试有 2 处编译错待修
  1. `run_cancel_retry_test.go` 中 `fakeEnq` 用了误造的 `ctx` alias → 改为 `context.Context`,并删除 `type ctx = ...` 那行
  2. `itoa` 未定义 → 改用同包已有的 `itoa64(int64) string`

详细 spec 见 `spec/tasks/M1.md`。

---

## 远端浏览器限制

如果在远端 / 沙箱环境运行:浏览器**无法访问本机 localhost**,视觉验证不可行。
改走 curl + typecheck + lint + build 验证路径。

---

## 已被拒的依赖

- `gorilla/websocket` — 改用 SSE
- `minio-go` — 改用 LocalFS

新依赖添加前先确认必要性。

---

## 仓库 & 凭据

- Git 分支: `main`
- bare repo HEAD: `f2cfdaa`,project id=1
- 测试 JWT: `/tmp/helios-e2e/jwt`
- 预编译 api binary: `/tmp/helios-api`
- 凭据值永不写入文件 / 不出现在 commit
