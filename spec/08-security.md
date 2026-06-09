# 08. 安全设计

## 8.1 认证 (Authentication)

| 方式 | 适用 |
|---|---|
| **用户名密码** + JWT | Web UI 默认 |
| **OIDC SSO** | 企业 (对接 Okta / Auth0 / Authing / 自建 Keycloak) |
| **Personal Access Token (PAT)** | CLI / API 自动化 |
| **Project Token** | 项目级 webhook / 第三方集成 |

JWT 配置:
- 算法: RS256 (非对称,公钥放 `/.well-known/jwks.json`)
- TTL: access 30min,refresh 7d
- 包含: `sub`, `org_id`, `roles`, `permissions[]`, `exp`, `jti`
- 失效: jti 黑名单存 Redis (TTL=token 剩余时长)

## 8.2 授权 (Authorization)

### RBAC 模型 (Casbin)

**资源层级**: Org > Project > Pipeline > Run

**内置角色**:
| 角色 | 权限 |
|---|---|
| `org_owner` | 全部 (含组织管理) |
| `org_admin` | 组织内全部 (不含删除组织) |
| `project_admin` | 项目内全部 |
| `project_developer` | 项目内编辑流水线、触发、查看 |
| `project_viewer` | 只读 |
| `approver` | 审批权限 |

**Casbin 策略示例**:
```
p, project_developer, project:{pid}/*, read
p, project_developer, project:{pid}/pipeline/*, write
p, project_developer, project:{pid}/pipeline/*/trigger, execute
p, project_viewer,    project:{pid}/*, read
p, approver,          project:{pid}/run/*/approval, execute
```

### 资源配额
- 单组织: 流水线数、并发 run 数、月构建时长
- 单流水线: max parallel jobs / max runtime / max log size
- 超额触发限流或邮件提醒

## 8.3 密钥管理

### 三层加密
```
明文 secret
  ↓ AES-256-GCM (DEK, 每条独立)
密文 + DEK 加密的 ciphertext
  ↓ KMS / Vault 加密 DEK
DEK ciphertext (随密文一起存)
↓ master key (KEK) 托管在 KMS,永不出环境
```

### 注入方式
- 容器化执行: 通过环境变量 / 文件挂载注入到 runner 容器
- SSH 执行: 临时环境变量传递
- 日志屏蔽: 输出中自动 mask secret 值 (替换为 `***`)

### 访问控制
- 按 scope 隔离 (org / project / pipeline)
- 流水线必须声明使用哪些 secret (`secrets: [DB_PASSWORD, API_KEY]`)
- 审计: 每次解密都记录 (谁 / 何时 / 哪个 pipeline / 哪个 run)

## 8.4 执行隔离

### K8s Runner
- 每个 run 独立 Namespace (`helios-run-{run_id}`)
- NetworkPolicy: 仅允许出方向到目标 cluster / git server / registry
- PodSecurityPolicy: 禁用 hostNetwork / hostPID / privileged (除非显式声明)
- Resource Limits: 强制 CPU/Mem 上限
- Run 结束自动 cleanup namespace

### Docker Runner
- 独立容器,容器外文件系统不可见
- 限制 capabilities (drop ALL, add 必需)
- seccomp default profile

### SSH Runner (高风险)
- 必须使用专用低权限用户 (`helios-runner`)
- 命令白名单 (可配置)
- sudo 命令必须显式批准
- 全部命令记录到 audit

## 8.5 网络与传输

- 全站 HTTPS,内部服务 mTLS (cert-manager)
- Webhook 校验签名 (HMAC-SHA256)
- API 限流 (按 user/IP/endpoint)
- CORS 白名单 (UI domain only)

## 8.6 漏洞与合规

- 镜像扫描: Trivy / Grype (流水线内置 + 平台镜像自检)
- SBOM 生成: syft (随制品保存)
- 容器签名: cosign (Sigstore)
- 依赖审计: dependabot / renovate (代码侧推动)
- 合规: SOC 2 / ISO 27001 友好的审计日志 (3 年保留 + 不可篡改)

## 8.7 审计日志

记录所有写操作 + 关键读操作:
- 用户登录 / 登出 / 失败
- 项目 / 流水线 CRUD
- Secret CRUD + 解密访问
- 集群接入 / 解绑
- 部署执行 / 回滚
- 权限变更

存储不可篡改: append-only,定期归档到 S3 + Object Lock。
