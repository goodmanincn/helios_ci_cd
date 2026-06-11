# Changelog

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)。

## [1.0.0-beta.1] - 2026-06-11

### Added

- 流水线模板市场：5 个内置模板 + Web/CLI 克隆
- `helios` CLI：认证、项目、runs、pipelines、secrets、clusters、hosts、templates
- 文档站 `docs-site/`（Docusaurus，中文）
- 主机 SSH 公钥分发 API `POST /hosts/:id/dispatch-key`
- 压测脚本 `scripts/perf/`（k6，4 场景）
- goreleaser 配置与 `scripts/install-helios.sh`

### Changed

- Node/Docker/K8s 内置模板增加审批与 rolling 部署链
- Go 模板扩展为 6 平台矩阵 + GitHub Release 步骤

### Known limitations (Beta)

- CLI `login` 为用户名/密码，OAuth 浏览器流待 v1.1
- 部分云插件 step 需插件市场安装后可用
- 压测基线报告待生产规格环境实测

[1.0.0-beta.1]: https://github.com/helios-cicd/helios/compare/v0.9.0...v1.0.0-beta.1
