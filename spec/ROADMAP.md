# Helios CI/CD — 项目路线图 (ROADMAP)

> 版本: v0.1
> 日期: 2026-06-09
> 团队: 1 人全栈 · 8h/d · 5d/w
> 目标定位: 个人 / 小团队自用
> 技术栈: Go (Gin + GORM + Asynq) · Next.js 14 (React Flow) · PostgreSQL · Redis · MinIO

---

## 总体节奏

| Milestone | 名称 | 工期 | 周次 | 类型 |
|---|---|---|---|---|
| M0 | 地基 | 2w | W1–W2 | 基础 |
| M1 | 极简闭环 | 4w | W3–W6 | MVP 前置 |
| M2 | 流水线引擎 | 3w | W7–W9 | MVP 前置 |
| **M3** | **可视化编辑器** | **4w** | **W10–W13** | **★ MVP 完成** |
| M4 | K8s 部署 | 3w | W14–W16 | v1.0 |
| M5 | 多云对接 | 3w | W17–W19 | v1.0 |
| M6 | 物理机 / SSH | 2w | W20–W21 | v1.0 |
| M7 | 生产化 | 3w | W22–W24 | v1.0 |
| **M8** | **Beta 收尾** | **2w** | **W25–W26** | **★ v1.0 Beta** |
| M9 | 插件市场 | 4w | W27–W30 | MVP 后追加 |

**关键里程碑:**
- **W13 / 第 3 个月底** — MVP 完成,可邀请第一批用户试用
- **W26 / 第 6 个月底** — v1.0 Beta 发布
- **W30 / 第 7 个月底** — 含插件市场的完整版

**理想 vs 实际:**
- 单人理想工期:7 个月
- 实际预期 (+30% 缓冲):**MVP 4 个月 / v1.0 Beta 8 个月 / 完整版 9 个月**

---

## 依赖关系图

```
  M0 ──► M1 ──► M2 ──► M3  ★ MVP
                       │
                       ├─► M4 ──► M5
                       │         │
                       │         └─► M7 ──► M8  ★ v1.0 Beta
                       │              ▲
                       └─► M6 ────────┘

  M9 (插件市场) — MVP 后任意时机插入,仅依赖 M2
```

---

## Milestone 详述

### M0 · 地基 (2w · W1–W2)
**目标**: 写第一行业务代码前所需的全部脚手架就位
**Epics**:
- E0.1 仓库与开发环境 (monorepo / lint / docker-compose / Makefile)
- E0.2 数据库与迁移 (14 张表 DDL + seed)
- E0.3 认证地基 (JWT + 登录登出 + 前端 auth)

**完成定义**: `make dev` 一键起本地环境,能登录看到空仪表盘

---

### M1 · 极简闭环 (4w · W3–W6)
**目标**: GitHub push → 拉代码 → 跑 shell 命令 → UI 实时看日志
**Epics**:
- E1.1 项目 CRUD
- E1.2 Git 集成 (先 GitHub 一家)
- E1.3 简化版引擎 (单 stage 单 step)
- E1.4 Docker Runner
- E1.5 实时日志 (gRPC + Redis Stream + WS)
- E1.6 执行详情页

**完成定义**: GitHub push 代码,UI 看到日志实时滚动到结束
**风险**: 最难的 milestone,涉及面最广,顶住

---

### M2 · 流水线引擎 (3w · W7–W9)
**目标**: 用 YAML 描述多 stage DAG 并跑通
**Epics**:
- E2.1 DSL 解析与校验
- E2.2 DAG 调度 (拓扑排序 + 并行 + 矩阵)
- E2.3 表达式引擎 (`${{ }}` + 函数)
- E2.4 制品传递 (upload/download artifact)
- E2.5 密钥保险箱
- E2.6 人工审批

**完成定义**: 5 stage YAML (含矩阵+审批+密钥) 完整跑通

---

### M3 · 可视化编辑器 (4w · W10–W13) ★ MVP
**目标**: 拖拽生成 YAML / YAML 反解为图
**Epics**:
- E3.1 React Flow 集成
- E3.2 节点面板 (左侧步骤库)
- E3.3 属性面板 (右侧表单)
- E3.4 YAML ↔ 图 双向转换
- E3.5 校验集成 (实时高亮错误)
- E3.6 版本管理

**完成定义**: 拖出一条新流水线并跑通 / 改 YAML 自动同步到画布
**风险**: 单人 4 周激进。降级方案: 先做 monaco YAML 编辑模式 + 校验,拖拽编辑器单独迭代

★ **此处达到 MVP**,可对外演示

---

### M4 · K8s 部署 (3w · W14–W16)
**Epics**:
- E4.1 ClusterProvider 接口 + 自建 K8s 实现
- E4.2 集群接入向导 + 连通性测试
- E4.3 k8s-deploy 内置 step
- E4.4 Rolling / Recreate 策略
- E4.5 部署历史 + 一键回滚
- E4.6 集群资源看板

---

### M5 · 多云对接 (3w · W17–W19)
**Epics**:
- E5.1 腾讯 TKE provider (CAM + STS)
- E5.2 阿里 ACK provider (RAM + AK/SK)
- E5.3 凭据管理 UI
- E5.4 跨云矩阵部署示例
- E5.5 集群健康监控 + 事件流

---

### M6 · 物理机 / SSH (2w · W20–W21)
**Epics**:
- E6.1 主机 CRUD + 主机组
- E6.2 SSH Runner
- E6.3 ssh-deploy 内置 step (rsync + remote exec)
- E6.4 滚动并发控制

---

### M7 · 生产化 (3w · W22–W24)
**Epics**:
- E7.1 RBAC (Casbin) + 6 个内置角色
- E7.2 OIDC SSO
- E7.3 审计日志 (写库 + 归档 S3)
- E7.4 Helm Chart + 集群部署模式
- E7.5 Prometheus 指标 + Grafana dashboard
- E7.6 通知中心 (钉钉 / 邮件 / Webhook)

---

### M8 · Beta 收尾 (2w · W25–W26) ★ v1.0 Beta
**Epics**:
- E8.1 用户文档站 (docusaurus)
- E8.2 5 个开箱即用流水线模板
- E8.3 helios CLI 工具
- E8.4 性能压测 (500 并发 run)
- E8.5 v1.0 Beta 发布

---

### M9 · 插件市场 (4w · W27–W30) — MVP 后追加
**Epics**:
- E9.1 action.yml 契约 + 容器化执行器
- E9.2 插件 registry
- E9.3 插件市场 UI + 安装/版本管理
- E9.4 官方插件首批 10 个

---

## 任务清单导航

每个 milestone 的详细 task 拆解 (按 0.5–2 天粒度) 分文件存放:

- [tasks/M0.md](./tasks/M0.md) — 地基
- [tasks/M1.md](./tasks/M1.md) — 极简闭环
- [tasks/M2.md](./tasks/M2.md) — 流水线引擎
- [tasks/M3.md](./tasks/M3.md) — 可视化编辑器 (MVP)
- [tasks/M4.md](./tasks/M4.md) — K8s 部署
- [tasks/M5.md](./tasks/M5.md) — 多云对接
- [tasks/M6.md](./tasks/M6.md) — 物理机 / SSH
- [tasks/M7.md](./tasks/M7.md) — 生产化
- [tasks/M8.md](./tasks/M8.md) — Beta 收尾
- [tasks/M9.md](./tasks/M9.md) — 插件市场

每个 task 包含: ID / 标题 / 描述 / 估时 / 依赖 / 验收标准。

---

## 工作守则 (单人项目)

1. **不跨 milestone 跳跃** — 完成一个再开下一个,避免半成品堆积
2. **每个 milestone 结束留 0.5w 演示 + 总结** — 设计漂移防火墙
3. **每 2 周 retro 一次** — 是否要调整后续 milestone 范围
4. **难题超 2 天没进展就 ASK 或换方向** — 单人没人 unblock 你
5. **测试不打折扣** — 单人没人帮你 review,自动化测试是唯一安全网
6. **commit 频率 ≥ 每日** — 永远要有可回滚的检查点
