# v1.0.0-beta.1 发布检查清单

## 代码与 CI

- [ ] `make lint test` 全绿
- [ ] `go build ./...` api + worker + cli
- [ ] `cd web && pnpm build`
- [ ] `cd docs-site && pnpm build`

## 文档

- [x] 文档站内容（入门 / 手册 / 部署 / DSL）
- [x] CHANGELOG.md
- [x] KNOWN_ISSUES.md
- [ ] docs.helios.io 部署验证

## 发布物

- [ ] 镜像 `ghcr.io/helios-cicd/helios-api:v1.0.0-beta.1` 等
- [ ] Helm Chart 版本 bump + ArtifactHub
- [ ] `goreleaser release` CLI 六平台 + Homebrew tap
- [ ] 文档站版本 tag 部署

## 压测

- [x] k6 脚本就绪 (`scripts/perf/`)
- [ ] 四场景实测填入 `docs/perf/beta-baseline.md`

## 社区（T8.5.3）

- [ ] GitHub Discussions 开启
- [ ] 博客 / 社群帖文
- [ ] Demo 视频
