.PHONY: proto test clean help build-server package-agent package-agent-all package-plugins package-plugins-all package-all package-all-arch dev-docker-up dev-docker-up-d dev-docker-down dev-docker-logs dev-docker-restart

# 默认变量
VERSION ?= 1.0.0
SERVER_HOST ?= localhost:6751
GOARCH ?= amd64
GOOS ?= linux

# ============ 代码生成 ============

proto:
	@echo "Generating Protobuf Go code..."
	@if ! command -v protoc &> /dev/null; then \
		echo "Error: protoc not found. Please install protoc first."; \
		echo "macOS: brew install protobuf"; \
		echo "Ubuntu/Debian: sudo apt-get install protobuf-compiler"; \
		exit 1; \
	fi
	@if ! command -v protoc-gen-go &> /dev/null; then \
		echo "Installing protoc-gen-go..."; \
		go install google.golang.org/protobuf/cmd/protoc-gen-go@latest; \
	fi
	@if ! command -v protoc-gen-go-grpc &> /dev/null; then \
		echo "Installing protoc-gen-go-grpc..."; \
		go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest; \
	fi
	@./scripts/generate-proto.sh

# ============ 开发环境 ============

dev-docker-up:
	@echo "Starting Docker development environment..."
	@cd deploy && docker compose up --build

dev-docker-up-d:
	@echo "Starting Docker development environment in background..."
	@cd deploy && docker compose up -d --build

dev-docker-down:
	@echo "Stopping Docker development environment..."
	@cd deploy && docker compose down

dev-docker-logs:
	@cd deploy && docker compose logs -f

dev-docker-restart:
	@echo "Restarting services..."
	@cd deploy && docker compose restart manager ui

# ============ 构建打包 ============

build-server:
	@echo "Building server..."
	@mkdir -p dist/server
	@go build -ldflags "-s -w" -o dist/server/agentcenter ./cmd/server/agentcenter
	@go build -ldflags "-s -w" -o dist/server/manager ./cmd/server/manager
	@echo "Server binaries built: dist/server/"

package-agent:
	@./scripts/build.sh agent --arch=$(GOARCH) --version=$(VERSION) --server=$(SERVER_HOST)

package-agent-all:
	@./scripts/build.sh agent --arch=all --version=$(VERSION) --server=$(SERVER_HOST)

package-plugins:
	@./scripts/build.sh plugins --arch=$(GOARCH) --version=$(VERSION)

package-plugins-all:
	@./scripts/build.sh plugins --arch=all --version=$(VERSION)

package-all:
	@./scripts/build.sh all --arch=$(GOARCH) --version=$(VERSION) --server=$(SERVER_HOST)

package-all-arch:
	@./scripts/build.sh all --arch=all --version=$(VERSION) --server=$(SERVER_HOST)

# ============ 测试与质量 ============

test:
	go test ./...

fmt:
	go fmt ./...

lint:
	@if command -v golangci-lint &> /dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found, skipping lint"; \
	fi

# ============ 工具 ============

deps:
	go mod download
	go mod tidy

clean:
	find . -name "*.pb.go" -delete
	rm -rf dist/ bin/ tmp/
	rm -f agent agentcenter manager baseline collector baseline-plugin collector-plugin

certs:
	@echo "Generating certificates..."
	@./scripts/generate-certs.sh

# ============ 帮助 ============

help:
	@echo "MxSec Platform - Makefile Commands"
	@echo ""
	@echo "代码生成:"
	@echo "  make proto                  - 生成 Protobuf Go 代码"
	@echo ""
	@echo "开发环境 (Docker Compose):"
	@echo "  make dev-docker-up          - 启动开发环境 (前台，带日志)"
	@echo "  make dev-docker-up-d        - 启动开发环境 (后台)"
	@echo "  make dev-docker-down        - 停止开发环境"
	@echo "  make dev-docker-logs        - 查看日志"
	@echo "  make dev-docker-restart     - 重启服务 (manager + ui)"
	@echo ""
	@echo "构建打包:"
	@echo "  make build-server           - 构建 Server 二进制 (本地开发)"
	@echo "  make package-agent          - 打包 Agent (单架构 RPM/DEB)"
	@echo "  make package-agent-all      - 打包 Agent (amd64 + arm64)"
	@echo "  make package-plugins        - 构建所有插件 (单架构)"
	@echo "  make package-plugins-all    - 构建所有插件 (amd64 + arm64)"
	@echo "  make package-all            - 构建全部 (单架构)"
	@echo "  make package-all-arch       - 构建全部 (amd64 + arm64)"
	@echo ""
	@echo "测试与质量:"
	@echo "  make test                   - 运行测试"
	@echo "  make fmt                    - 格式化代码"
	@echo "  make lint                   - 代码检查"
	@echo ""
	@echo "工具:"
	@echo "  make deps                   - 下载依赖"
	@echo "  make clean                  - 清理构建产物"
	@echo "  make certs                  - 生成 mTLS 证书"
	@echo ""
	@echo "示例:"
	@echo "  make package-agent-all VERSION=1.0.5 SERVER_HOST=10.0.0.1:6751"
	@echo "  make package-all-arch VERSION=1.0.5 SERVER_HOST=10.0.0.1:6751"
	@echo ""
	@echo "输出目录:"
	@echo "  Agent RPM/DEB:  dist/packages/"
	@echo "  插件二进制:     dist/plugins/"
