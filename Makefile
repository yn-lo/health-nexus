# Health Nexus Go 后端 Makefile
# 用途: 提供构建、测试、lint、迁移、wire、sqlc、验证门禁等命令
# 约束对应: backend/.harness/specs/conventions/README.md

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ============================================================================
# 路径与版本
# ============================================================================
GO := go
GOFLAGS := -trimpath
# CURDIR 由 Make 自身提供，不 shell 出去；`$(shell pwd)` 在 Windows 上会失败并让 ROOT_DIR 为空。
ROOT_DIR := $(CURDIR)
BACKEND_GO := $(ROOT_DIR)/backend

# 工具版本（与 go.mod 对齐或固定）
GOLANGCI_VERSION := v1.62.0
WIRE_VERSION := v0.6.0
SQLC_VERSION := v1.27.0

# ============================================================================
# 帮助
# ============================================================================
.PHONY: help
help: ## 显示所有可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ============================================================================
# 构建
# ============================================================================
.PHONY: build
build: ## 编译 HTTP Server 与 asynq Worker
	cd $(BACKEND_GO) && $(GO) build $(GOFLAGS) -o bin/server ./cmd/server
	cd $(BACKEND_GO) && $(GO) build $(GOFLAGS) -o bin/worker ./cmd/worker

.PHONY: run
run: ## 本地运行 HTTP Server
	cd $(BACKEND_GO) && $(GO) run ./cmd/server

.PHONY: run-worker
run-worker: ## 本地运行 asynq Worker
	cd $(BACKEND_GO) && $(GO) run ./cmd/worker

# ============================================================================
# 测试
# ============================================================================
.PHONY: test
test: ## 运行所有单元测试
	cd $(BACKEND_GO) && $(GO) test -race -count=1 ./internal/...

.PHONY: test-integration
test-integration: ## 运行集成测试（需要 docker）
	cd $(BACKEND_GO) && $(GO) test -race -count=1 -tags=integration ./tests/integration/...

.PHONY: coverage
coverage: ## 生成覆盖率报告
	cd $(BACKEND_GO) && $(GO) test -race -coverprofile=coverage.out ./internal/...
	cd $(BACKEND_GO) && $(GO) tool cover -func=coverage.out | tail -1
	cd $(BACKEND_GO) && $(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: test-harness
test-harness: ## 运行 harness 架构约束测试（AC-ARCH-* AST 检查）
	cd $(BACKEND_GO) && $(GO) test ./internal/harness/arch/...

# ============================================================================
# Lint 与架构约束（验证门禁）
# ============================================================================
.PHONY: lint
lint: ## golangci-lint 全量检查
	cd $(BACKEND_GO) && golangci-lint run ./...

.PHONY: vet
vet: ## go vet
	cd $(BACKEND_GO) && $(GO) vet ./...

# 完整验证门禁：委托给唯一实现 backend/.harness/constraints/ci/gate.sh（P0+P1+P2）。
# 不再在本文件内重复定义检查范围，避免与 gate.sh 分叉成两套门禁。
.PHONY: verify
verify: ## 完整验证门禁（P0+P1+P2，委托 backend/.harness/constraints/ci/gate.sh）
	bash backend/.harness/constraints/ci/gate.sh

.PHONY: verify-strict
verify-strict: ## CI/发布门禁：被跳过的检查（缺工具/无 API Key）视为失败
	GATE_STRICT=1 bash backend/.harness/constraints/ci/gate.sh

.PHONY: govulncheck
govulncheck: ## 依赖漏洞扫描（仅报告，不阻断）
	cd $(BACKEND_GO) && govulncheck ./...

# ============================================================================
# 代码生成
# ============================================================================
.PHONY: wire
wire: ## wire 生成依赖注入代码
	cd $(BACKEND_GO) && wire ./internal/di/...

.PHONY: sqlc
sqlc: ## sqlc 生成类型安全 SQL 代码
	cd $(BACKEND_GO) && sqlc generate

# ============================================================================
# 工具安装（一次性）
# ============================================================================
.PHONY: tools
tools: ## 安装开发工具（golangci-lint / wire / sqlc）
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_VERSION)
	$(GO) install github.com/google/wire/cmd/wire@$(WIRE_VERSION)
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

# ============================================================================
# Docker
# ============================================================================
.PHONY: docker-up
docker-up: ## 启动所有服务（docker-compose）
	docker compose up -d

.PHONY: docker-down
docker-down: ## 停止所有服务
	docker compose down

.PHONY: docker-logs
docker-logs: ## 查看服务日志
	docker compose logs -f

# ============================================================================
# 清理
# ============================================================================
.PHONY: clean
clean: ## 清理构建产物
	rm -rf $(BACKEND_GO)/bin $(BACKEND_GO)/coverage.out $(BACKEND_GO)/coverage.html
