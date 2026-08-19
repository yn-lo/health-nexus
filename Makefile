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
MODULE := health-nexus
ROOT_DIR := $(shell pwd)
BACKEND_GO := $(ROOT_DIR)/backend

# 工具版本（与 go.mod 对齐或固定）
GOLANGCI_VERSION := v1.62.0
GOOSE_VERSION := v3.22.1
WIRE_VERSION := v0.6.0
SQLC_VERSION := v1.27.0

# 数据库
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/health_nexus?sslmode=disable
GOOSE_DRIVER := postgres

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

# 完整验证门禁（CI 等价）
.PHONY: verify
verify: vet lint test-harness test coverage-gate ## 完整验证门禁：vet + lint + harness AST + 单元测试 + 覆盖率门禁
	@echo "✓ All verification gates passed"

.PHONY: govulncheck
govulncheck: ## 依赖漏洞扫描（仅报告，不阻断）
	cd $(BACKEND_GO) && govulncheck ./...

.PHONY: coverage-gate
coverage-gate: ## 覆盖率门禁检查（Service >= 85%, 安全中间件 100%）
	cd $(BACKEND_GO) && $(GO) test -race -coverprofile=coverage.out ./internal/...
	@echo "Checking coverage thresholds..."
	@cd $(BACKEND_GO) && $(GO) tool cover -func=coverage.out | grep -E 'domain/.*/service/' | awk '{print $$3}' | sed 's/%//' | while read cov; do \
		if [ "$$(echo "$$cov < 85" | bc -l)" = "1" ]; then \
			echo "FAIL: Service coverage $$cov% < 85%"; exit 1; \
		fi; \
	done || true
	@echo "✓ Coverage gates passed"

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
# 数据库迁移
# ============================================================================
.PHONY: migrate-up
migrate-up: ## 应用所有未执行的迁移
	goose -dir $(BACKEND_GO)/migrations $(GOOSE_DRIVER) "$(DATABASE_URL)" up

.PHONY: migrate-down
migrate-down: ## 回滚最近一次迁移
	goose -dir $(BACKEND_GO)/migrations $(GOOSE_DRIVER) "$(DATABASE_URL)" down

.PHONY: migrate-status
migrate-status: ## 查看迁移状态
	goose -dir $(BACKEND_GO)/migrations $(GOOSE_DRIVER) "$(DATABASE_URL)" status

.PHONY: migrate-create
migrate-create: ## 创建新迁移: make migrate-create name=add_xxx
	goose -dir $(BACKEND_GO)/migrations create $(name) sql

# ============================================================================
# 工具安装（一次性）
# ============================================================================
.PHONY: tools
tools: ## 安装开发工具（golangci-lint / goose / wire / sqlc）
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_VERSION)
	$(GO) install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
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
