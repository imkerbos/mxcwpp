.PHONY: proto generate test clean help build-agent build-server package-agent docker-build docker-up docker-down

# 默认变量
VERSION ?= 1.0.0
SERVER_HOST ?= localhost:6751
GOARCH ?= amd64
GOOS ?= linux
DISTRO ?=  # 发行版：centos7, centos8, rocky8, rocky9, debian10, debian11, debian12 等
CERT_DIR ?= deploy/docker-compose/certs  # 证书目录

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

# 构建 Agent
build-agent:
	@echo "Building agent..."
	@BLS_SERVER_HOST=$(SERVER_HOST) BLS_VERSION=$(VERSION) GOARCH=$(GOARCH) GOOS=$(GOOS) ./scripts/build-agent.sh

# 构建 Server
build-server:
	@echo "Building server..."
	@mkdir -p dist/server
	@go build -ldflags "-s -w" -o dist/server/agentcenter ./cmd/server/agentcenter
	@go build -ldflags "-s -w" -o dist/server/manager ./cmd/server/manager
	@echo "Server binaries built: dist/server/"

# 打包 Agent（RPM/DEB）- 单架构
package-agent:
	@echo "Packaging agent ($(GOARCH))..."
	@BLS_SERVER_HOST=$(SERVER_HOST) BLS_VERSION=$(VERSION) BLS_DISTRO=$(DISTRO) BLS_CERT_DIR=$(CERT_DIR) GOARCH=$(GOARCH) GOOS=$(GOOS) ./scripts/package-agent.sh

# 打包 Agent（RPM/DEB）- 所有架构（amd64 + arm64）
package-agent-all:
	@echo "Packaging agent for all architectures..."
	@echo "=== Building amd64 packages ==="
	@BLS_SERVER_HOST=$(SERVER_HOST) BLS_VERSION=$(VERSION) BLS_DISTRO=$(DISTRO) BLS_CERT_DIR=$(CERT_DIR) GOARCH=amd64 GOOS=linux ./scripts/package-agent.sh
	@echo "=== Building arm64 packages ==="
	@BLS_SERVER_HOST=$(SERVER_HOST) BLS_VERSION=$(VERSION) BLS_DISTRO=$(DISTRO) BLS_CERT_DIR=$(CERT_DIR) GOARCH=arm64 GOOS=linux ./scripts/package-agent.sh
	@echo "All agent packages built successfully in dist/packages/"

# 构建插件（baseline + collector）
build-plugins:
	@echo "Building plugins..."
	@mkdir -p dist/plugins
	@echo "Building baseline plugin ($(GOARCH))..."
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "-s -w" -o dist/plugins/baseline ./plugins/baseline
	@echo "Building collector plugin ($(GOARCH))..."
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "-s -w" -o dist/plugins/collector ./plugins/collector
	@echo "Plugins built: dist/plugins/"

# 构建插件 - 所有架构（amd64 + arm64）
build-plugins-all:
	@echo "Building plugins for all architectures..."
	@mkdir -p dist/plugins/amd64 dist/plugins/arm64
	@echo "=== Building amd64 plugins ==="
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o dist/plugins/amd64/baseline ./plugins/baseline
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o dist/plugins/amd64/collector ./plugins/collector
	@echo "=== Building arm64 plugins ==="
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o dist/plugins/arm64/baseline ./plugins/baseline
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o dist/plugins/arm64/collector ./plugins/collector
	@echo "All plugins built in dist/plugins/{amd64,arm64}/"

# 打包 Server（RPM/DEB）
package-server:
	@echo "Packaging server..."
	@BLS_VERSION=$(VERSION) BLS_DISTRO=$(DISTRO) GOARCH=$(GOARCH) GOOS=$(GOOS) ./scripts/package-server.sh

# 打包所有（Agent + Server）
package-all: package-agent package-server
	@echo "All packages built successfully"

# Docker 相关命令
docker-build:
	@echo "Building Docker images..."
	@cd deploy/docker-compose && docker-compose build

docker-up:
	@echo "Starting Docker services..."
	@cd deploy/docker-compose && docker-compose up -d

docker-down:
	@echo "Stopping Docker services..."
	@cd deploy/docker-compose && docker-compose down

docker-logs:
	@cd deploy/docker-compose && docker-compose logs -f

docker-ps:
	@cd deploy/docker-compose && docker-compose ps

docker-restart:
	@echo "Restarting Docker services..."
	@cd deploy/docker-compose && docker-compose restart

docker-clean:
	@echo "Cleaning Docker resources..."
	@cd deploy/docker-compose && docker-compose down -v
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
	@cd deploy/docker-compose && docker-compose -f docker-compose.dev.yml up -d --build agentcenter manager ui agent

# Docker 开发环境停止
dev-docker-down:
	@echo "Stopping Docker development environment..."
	@cd deploy/docker-compose && docker-compose -f docker-compose.dev.yml down

# Docker 开发环境日志
dev-docker-logs:
	@cd deploy/docker-compose && docker-compose -f docker-compose.dev.yml logs -f

# Docker 开发环境重启
dev-docker-restart:
	@cd deploy/docker-compose && docker-compose -f docker-compose.dev.yml restart manager ui

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
	@echo "  make generate       - Alias for proto"
	@echo ""
	@echo "构建:"
	@echo "  make build-agent      - Build agent binary (SERVER_HOST=host:port VERSION=1.0.0)"
	@echo "  make build-server     - Build server binaries (agentcenter, manager)"
	@echo "  make build-plugins    - Build plugins (baseline, collector) for current arch"
	@echo "  make build-plugins-all- Build plugins for all architectures (amd64 + arm64)"
	@echo "  make package-agent    - Package agent as RPM/DEB for current arch"
	@echo "  make package-agent-all- Package agent as RPM/DEB for all architectures"
	@echo "  make package-server   - Package server as RPM/DEB (VERSION=1.0.0)"
	@echo "  make package-all      - Package both agent and server"
	@echo ""
	@echo "安装:"
	@echo "  make install-agent  - Install agent from package (VERSION=1.0.0)"
	@echo "  make install-server - Install server from package (VERSION=1.0.0)"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build   - Build Docker images"
	@echo "  make docker-up      - Start Docker services"
	@echo "  make docker-down    - Stop Docker services"
	@echo "  make docker-logs    - Show Docker logs"
	@echo "  make docker-ps      - Show Docker service status"
	@echo "  make docker-restart - Restart Docker services"
	@echo "  make docker-clean   - Clean Docker resources (including volumes)"
	@echo ""
	@echo "开发环境:"
	@echo "  make dev-up         - Start development environment (build + up)"
	@echo "  make dev-down       - Stop development environment"
	@echo ""
	@echo "测试与质量:"
	@echo "  make test           - Run tests"
	@echo "  make fmt            - Format code"
	@echo "  make lint           - Run linter"
	@echo ""
	@echo "工具:"
	@echo "  make deps           - Download and tidy dependencies"
	@echo "  make certs          - Generate mTLS certificates"
	@echo "  make clean          - Clean generated files"
	@echo "  make help           - Show this help message"
	@echo ""
	@echo "示例:"
	@echo "  make build-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0"
	@echo "  make package-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0 DISTRO=rocky9"
	@echo "  make package-agent-all SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0"
	@echo "  make package-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0 CERT_DIR=./certs"
	@echo "  make package-server VERSION=1.0.0 DISTRO=debian12"
	@echo "  make build-plugins GOARCH=arm64"
	@echo "  make build-plugins-all"
	@echo "  make dev-up         # Start development environment"
	@echo ""
	@echo "支持的发行版 (DISTRO):"
	@echo "  RPM: centos7, centos8, centos9, rocky8, rocky9, el7, el8, el9"
	@echo "  DEB: debian10, debian11, debian12, ubuntu20, ubuntu22"
	@echo ""
	@echo "证书打包说明:"
	@echo "  默认从 deploy/docker-compose/certs/ 读取证书"
	@echo "  需要: ca.crt, client.crt, client.key"
	@echo "  生成证书: make certs"
	@echo ""
	@echo "注意: Rocky Linux 9 和 CentOS Stream 9 可以共用 el9 包"
	@echo ""
	@echo "插件管理说明:"
	@echo "  1. 编译插件: make build-plugins-all"
	@echo "  2. 将插件上传到 Manager: 系统管理 > 组件列表"
	@echo "  3. Agent 会自动通过 gRPC 获取插件配置并下载更新"
	@echo "  4. 更新策略: 基于版本号比较，新版本自动替换旧版本"
