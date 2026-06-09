# Helios — 顶层开发命令
# 用法: make help

SHELL := /bin/bash
.DEFAULT_GOAL := help

# === 配置 ===
COMPOSE := docker compose -f deploy/docker/dev.compose.yml
GO_MODULES := api worker runner cli
API_PORT ?= 8080
WEB_PORT ?= 3000

# 版本注入
VERSION  ?= dev
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_AT := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -X main.Version=$(VERSION) -X main.GitCommit=$(COMMIT) -X main.BuildTime=$(BUILD_AT)

# 颜色
CYAN := \033[36m
GREEN := \033[32m
YELLOW := \033[33m
RESET := \033[0m

# === 帮助 ===
.PHONY: help
help:  ## 显示帮助
	@printf "$(CYAN)Helios 开发命令$(RESET)\n\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  $(GREEN)%-18s$(RESET) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n$(YELLOW)常用工作流:$(RESET)\n"
	@printf "  首次启动:  make setup && make dev\n"
	@printf "  日常开发:  make dev\n"
	@printf "  提交前:    make lint test\n"
	@printf "  重置:      make clean\n\n"

# === 环境 ===
.PHONY: setup
setup:  ## 首次环境准备 (复制 .env / 安装 hooks)
	@test -f .env || cp .env.example .env && echo "✓ .env 已创建,请按需修改"
	@command -v golangci-lint >/dev/null 2>&1 || (echo "→ 安装 golangci-lint" && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	@command -v gofumpt >/dev/null 2>&1 || (echo "→ 安装 gofumpt" && go install mvdan.cc/gofumpt@latest)
	@command -v migrate >/dev/null 2>&1 || (echo "→ 安装 golang-migrate" && go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest)
	@echo "✓ setup 完成"

# === 依赖服务 ===
.PHONY: dev-deps
dev-deps:  ## 拉起 PG / Redis / MinIO / Adminer
	@$(COMPOSE) up -d
	@echo "✓ dev 依赖已启动"
	@$(COMPOSE) ps

.PHONY: dev-deps-down
dev-deps-down:  ## 停止 dev 依赖
	@$(COMPOSE) down

.PHONY: dev-deps-clean
dev-deps-clean:  ## 停止并清除 dev 数据卷 (⚠️ 数据丢失)
	@$(COMPOSE) down -v
	@echo "✓ dev 数据已清空"

# === 开发主入口 ===
.PHONY: dev
dev: dev-deps  ## 一键启动 (依赖 + API + Web)
	@printf "$(YELLOW)启动 API ($(API_PORT)) 和 Web ($(WEB_PORT))...$(RESET)\n"
	@printf "Ctrl+C 停止\n\n"
	@trap 'kill 0' EXIT INT TERM; \
		(cd api && HELIOS_API_ADDR=:$(API_PORT) go run ./cmd/api 2>&1 | sed "s/^/[$(CYAN)api$(RESET)] /") & \
		(test -d web && cd web && PORT=$(WEB_PORT) pnpm dev 2>&1 | sed "s/^/[$(GREEN)web$(RESET)] /" || echo "[web] web/ 尚未初始化,跳过") & \
		wait

.PHONY: dev-api
dev-api: dev-deps  ## 只跑 API
	@cd api && HELIOS_API_ADDR=:$(API_PORT) go run ./cmd/api

.PHONY: dev-web
dev-web:  ## 只跑 Web
	@cd web && PORT=$(WEB_PORT) pnpm dev

# === Lint ===
.PHONY: lint
lint: lint-go lint-web  ## 全部 lint

.PHONY: lint-go
lint-go:  ## Go lint (所有模块)
	@for m in $(GO_MODULES); do \
		printf "$(CYAN)→ lint $$m$(RESET)\n"; \
		(cd $$m && golangci-lint run ./... --config=../.golangci.yml) || exit 1; \
	done

.PHONY: lint-web
lint-web:  ## Web lint
	@test -d web && cd web && pnpm lint || echo "(web/ 未初始化,跳过)"

.PHONY: fmt
fmt:  ## 格式化所有 Go 代码
	@for m in $(GO_MODULES); do \
		(cd $$m && gofumpt -w . && goimports -w -local github.com/helios-cicd/helios .); \
	done
	@echo "✓ format 完成"

# === 测试 ===
.PHONY: test
test: test-go test-web  ## 全部测试

.PHONY: test-go
test-go:  ## Go 单元测试 (含覆盖率)
	@for m in $(GO_MODULES); do \
		printf "$(CYAN)→ test $$m$(RESET)\n"; \
		(cd $$m && go test -race -coverprofile=coverage.out -covermode=atomic ./...) || exit 1; \
	done

.PHONY: test-web
test-web:  ## Web 测试
	@test -d web && cd web && pnpm test 2>/dev/null || echo "(web 测试尚未配置,跳过)"

# === 构建 ===
.PHONY: build
build: build-go build-web  ## 全部构建

.PHONY: build-go
build-go:  ## 构建所有 Go 二进制到 bin/
	@mkdir -p bin
	@for m in $(GO_MODULES); do \
		printf "$(CYAN)→ build $$m$(RESET)\n"; \
		(cd $$m && go build -ldflags "$(LDFLAGS)" -o ../bin/helios-$$m ./cmd/...) || exit 1; \
	done
	@ls -lh bin/

.PHONY: build-web
build-web:  ## 构建 Web 静态资源
	@test -d web && cd web && pnpm build || echo "(web/ 未初始化,跳过)"

# === 迁移 (M0.2 完成后启用) ===
.PHONY: migrate-up
migrate-up:  ## 应用所有 pending 数据库迁移
	@migrate -path db/migrations -database "$$HELIOS_DB_DSN" up

.PHONY: migrate-down
migrate-down:  ## 回滚最近一次迁移
	@migrate -path db/migrations -database "$$HELIOS_DB_DSN" down 1

.PHONY: migrate-create
migrate-create:  ## 新建迁移 (NAME=add_xxx)
	@test -n "$(NAME)" || (echo "用法: make migrate-create NAME=add_xxx"; exit 1)
	@migrate create -ext sql -dir db/migrations -seq $(NAME)

.PHONY: seed
seed:  ## 灌入种子数据 (M0.2 后启用)
	@test -f api/cmd/seed/main.go && (cd api && go run ./cmd/seed) || echo "(seed 尚未实现,跳过)"

# === 清理 ===
.PHONY: clean
clean:  ## 清理构建产物 (不动数据)
	@rm -rf bin/ dist/
	@find . -name coverage.out -delete
	@for m in $(GO_MODULES); do (cd $$m && go clean -cache 2>/dev/null || true); done
	@echo "✓ 构建产物已清"

.PHONY: nuke
nuke: clean dev-deps-clean  ## 终极清理 (含数据!)
	@rm -rf web/node_modules web/.next
	@echo "✓ 全部清空"

# === 工具检查 ===
.PHONY: doctor
doctor:  ## 检查开发环境
	@echo "=== 工具检查 ==="
	@command -v go >/dev/null && go version || echo "✗ Go 未安装"
	@command -v node >/dev/null && node --version | xargs -I{} echo "Node {}" || echo "✗ Node 未安装"
	@command -v pnpm >/dev/null && echo "pnpm $$(pnpm --version)" || echo "✗ pnpm 未安装"
	@command -v docker >/dev/null && docker --version || echo "✗ Docker 未安装"
	@command -v golangci-lint >/dev/null && echo "golangci-lint $$(golangci-lint --version | head -1)" || echo "⚠ golangci-lint 未安装 (运行 make setup)"
	@command -v migrate >/dev/null && echo "migrate $$(migrate -version 2>&1)" || echo "⚠ migrate 未安装 (运行 make setup)"
	@echo ""
	@echo "=== 服务状态 ==="
	@$(COMPOSE) ps 2>/dev/null || echo "(dev-deps 未启动)"
