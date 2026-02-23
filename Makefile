.PHONY: proto generate test clean help build-server package-agent package-agent-all package-plugins package-plugins-all package-fim package-all package-all-arch docker-build docker-up docker-down

# 默认变量
VERSION ?= 1.0.0
SERVER_HOST ?= localhost:6751
GOARCH ?= amd64
GOOS ?= linux
DISTRO ?=  # 发行版：centos7, centos8, rocky8, rocky9, debian10, debian11, debian12 等
CERT_DIR ?= deploy/dev/certs  # 证书目录

# 生成 Protobuf Go 代码
proto: generate

generate:
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

# 运行测试
test:
	go test ./...

# 格式化代码
fmt:
	go fmt ./...

# 代码检查
lint:
	@if command -v golangci-lint &> /dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found, skipping lint"; \
	fi

# 清理生成的文件
clean:
	find . -name "*.pb.go" -delete
	rm -rf dist/

# 下载依赖
deps:
	go mod download
	go mod tidy

# 构建 Server（本地二进制，用于开发）
build-server:
	@echo "Building server..."
	@mkdir -p dist/server
	@go build -ldflags "-s -w" -o dist/server/agentcenter ./cmd/server/agentcenter
	@go build -ldflags "-s -w" -o dist/server/manager ./cmd/server/manager
	@echo "Server binaries built: dist/server/"

# ============ 统一打包命令 ============
# Agent: 输出 RPM/DEB 系统包
# 插件: 输出二进制文件（由 Agent 动态管理）

# 打包 Agent（单架构）
package-agent:
	@./scripts/build.sh agent --arch=$(GOARCH) --version=$(VERSION) --server=$(SERVER_HOST)

# 打包 Agent（所有架构）
package-agent-all:
	@./scripts/build.sh agent --arch=all --version=$(VERSION) --server=$(SERVER_HOST)

# 构建 Baseline 插件（单架构）- 输出二进制文件
package-baseline:
	@./scripts/build.sh baseline --arch=$(GOARCH) --version=$(VERSION)

# 构建 Collector 插件（单架构）- 输出二进制文件
package-collector:
	@./scripts/build.sh collector --arch=$(GOARCH) --version=$(VERSION)

# 构建 FIM 插件（单架构）- 输出二进制文件
package-fim:
	@./scripts/build.sh fim --arch=$(GOARCH) --version=$(VERSION)

# 构建所有插件（单架构）- 输出二进制文件
package-plugins:
	@./scripts/build.sh plugins --arch=$(GOARCH) --version=$(VERSION)

# 构建所有插件（所有架构）- 输出二进制文件
package-plugins-all:
	@./scripts/build.sh plugins --arch=all --version=$(VERSION)

# 构建所有（Agent RPM/DEB + 插件二进制，单架构）
package-all:
	@./scripts/build.sh all --arch=$(GOARCH) --version=$(VERSION) --server=$(SERVER_HOST)

# 构建所有（Agent RPM/DEB + 插件二进制，所有架构）
package-all-arch:
	@./scripts/build.sh all --arch=all --version=$(VERSION) --server=$(SERVER_HOST)

# Docker 相关命令
docker-build:
	@echo "Building Docker images..."
	@cd deploy/dev && docker-compose build

docker-up:
	@echo "Starting Docker services..."
	@cd deploy/dev && docker-compose up -d

docker-down:
	@echo "Stopping Docker services..."
	@cd deploy/dev && docker-compose down

docker-logs:
	@cd deploy/dev && docker-compose logs -f

docker-ps:
	@cd deploy/dev && docker-compose ps

docker-restart:
	@echo "Restarting Docker services..."
	@cd deploy/dev && docker-compose restart

docker-clean:
	@echo "Cleaning Docker resources..."
	@cd deploy/dev && docker-compose down -v
	@docker system prune -f

# 生成证书
certs:
	@echo "Generating certificates..."
	@./scripts/generate-certs.sh

# 安装 Agent（从 RPM/DEB 包）
install-agent:
	@echo "Installing agent..."
	@if [ -f dist/packages/mxcsec-agent-$(VERSION)-*.rpm ]; then \
		sudo rpm -ivh dist/packages/mxcsec-agent-$(VERSION)-*.rpm; \
	elif [ -f dist/packages/mxcsec-agent_$(VERSION)_*.deb ]; then \
		sudo dpkg -i dist/packages/mxcsec-agent_$(VERSION)_*.deb; \
	else \
		echo "Error: No package found. Run 'make package-agent' first."; \
		exit 1; \
	fi

# 安装 Server（从 RPM/DEB 包）
install-server:
	@echo "Installing server..."
	@if [ -f dist/packages/mxsec-server-$(VERSION)-*.rpm ]; then \
		sudo rpm -ivh dist/packages/mxsec-server-$(VERSION)-*.rpm; \
	elif [ -f dist/packages/mxsec-server_$(VERSION)_*.deb ]; then \
		sudo dpkg -i dist/packages/mxsec-server_$(VERSION)_*.deb; \
	else \
		echo "Error: No package found. Run 'make package-server' first."; \
		exit 1; \
	fi

# 部署开发环境（一键启动Docker服务）
dev-up: docker-build docker-up
	@echo "Development environment started"
	@echo "MySQL: localhost:3306"
	@echo "AgentCenter: localhost:6751"
	@echo "Manager: http://localhost:8080"

# 部署开发环境（停止Docker服务）
dev-down: docker-down
	@echo "Development environment stopped"

# 本地开发启动（后端+前端）- 宿主机模式
dev-start:
	@echo "Starting local development environment..."
	@./scripts/dev-start.sh

# Docker 开发环境启动（推荐，模拟 Linux 环境）
dev-docker-up:
	@echo "Starting Docker development environment..."
	@./scripts/dev-docker-start.sh

# Docker 开发环境启动（后台模式）
dev-docker-up-d:
	@echo "Starting Docker development environment in background..."
	@cd deploy/dev && docker-compose -f docker-compose.dev.yml up -d --build agentcenter manager ui agent

# Docker 开发环境停止
dev-docker-down:
	@echo "Stopping Docker development environment..."
	@cd deploy/dev && docker-compose -f docker-compose.dev.yml down

# Docker 开发环境日志
dev-docker-logs:
	@cd deploy/dev && docker-compose -f docker-compose.dev.yml logs -f

# Docker 开发环境重启
dev-docker-restart:
	@cd deploy/dev && docker-compose -f docker-compose.dev.yml restart manager ui

# 本地开发启动（仅后端）- 宿主机模式
dev-server:
	@echo "Starting backend server..."
	@if [ ! -f configs/server.yaml ]; then \
		cp configs/server.yaml.example configs/server.yaml; \
	fi
	@make build-server
	@./dist/server/manager -config configs/server.yaml

# 安装前端依赖
ui-deps:
	@echo "Installing UI dependencies..."
	@cd ui && npm install

# 本地开发启动（仅前端）
dev-ui:
	@echo "Starting frontend UI..."
	@cd ui && npm run dev

# 初始化数据库（创建mxsec数据库）
init-db:
	@echo "Initializing database..."
	@./scripts/init-db.sh

# 帮助信息
help:
	@echo "Available targets:"
	@echo ""
	@echo "代码生成:"
	@echo "  make proto          - Generate Protobuf Go code"
	@echo ""
	@echo "构建:"
	@echo "  Agent (输出 RPM/DEB 系统包):"
	@echo "    make package-agent       - 打包 Agent (单架构)"
	@echo "    make package-agent-all   - 打包 Agent (amd64 + arm64)"
	@echo ""
	@echo "  插件 (输出二进制文件，由 Agent 动态管理):"
	@echo "    make package-baseline    - 构建 Baseline 插件 (单架构)"
	@echo "    make package-collector   - 构建 Collector 插件 (单架构)"
	@echo "    make package-fim         - 构建 FIM 插件 (单架构)"
	@echo "    make package-plugins     - 构建所有插件 (单架构)"
	@echo "    make package-plugins-all - 构建所有插件 (amd64 + arm64)"
	@echo ""
	@echo "  全部构建:"
	@echo "    make package-all         - Agent RPM/DEB + 插件二进制 (单架构)"
	@echo "    make package-all-arch    - Agent RPM/DEB + 插件二进制 (amd64 + arm64)"
	@echo ""
	@echo "开发构建:"
	@echo "  make build-server   - 构建 Server 二进制 (本地开发用)"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-up      - Start Docker services"
	@echo "  make docker-down    - Stop Docker services"
	@echo "  make docker-logs    - Show Docker logs"
	@echo "  make dev-docker-up  - Start Docker dev environment"
	@echo ""
	@echo "测试与质量:"
	@echo "  make test           - Run tests"
	@echo "  make fmt            - Format code"
	@echo "  make lint           - Run linter"
	@echo ""
	@echo "工具:"
	@echo "  make deps           - Download dependencies"
	@echo "  make certs          - Generate mTLS certificates"
	@echo "  make clean          - Clean generated files"
	@echo ""
	@echo "示例:"
	@echo "  make package-agent-all VERSION=1.0.5 SERVER_HOST=10.0.0.1:6751"
	@echo "  make package-plugins-all VERSION=1.0.5"
	@echo "  make package-all-arch VERSION=1.0.5 SERVER_HOST=10.0.0.1:6751"
	@echo ""
	@echo "输出目录:"
	@echo "  Agent RPM/DEB:  dist/packages/"
	@echo "  插件二进制:     dist/plugins/"
