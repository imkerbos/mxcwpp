# 开发环境搭建

## 前置要求

- Go >= 1.21
- Node.js >= 18.x
- MySQL 8.0+
- protoc (protobuf 编译器)
- Git

## 项目结构

```
cmd/                    # 主程序入口 (agent, agentcenter, manager)
internal/               # 内部包 (agent/, server/)
plugins/                # 插件 (baseline, collector, fim)
api/proto/              # Protobuf 定义
ui/                     # Vue 3 前端
deploy/                 # 部署配置
configs/                # 配置文件
scripts/                # 脚本工具
docs/                   # 文档
tests/                  # 集成测试与性能测试
```

## 方式一: Docker 开发环境（推荐）

```bash
make dev-docker-up       # 启动（带热更新）
make dev-docker-logs     # 查看日志
make dev-docker-down     # 停止
```

当前开发环境使用单节点 Kafka KRaft；压测环境使用 3 节点 Kafka KRaft。

## 方式二: 本地开发

### 1. 配置数据库

创建 MySQL 数据库 `mxsec`，表结构由 Gorm AutoMigrate 自动创建。

### 2. 配置文件

```bash
cp configs/server.yaml.example configs/server.yaml
vim configs/server.yaml   # 配置数据库连接等
```

### 3. 生成证书

```bash
./scripts/generate-certs.sh
```

### 4. 启动后端

```bash
# 启动 AgentCenter
go run cmd/server/agentcenter/main.go

# 启动 Manager（另一个终端）
go run cmd/server/manager/main.go
```

### 5. 启动前端

```bash
cd ui
npm install
npm run dev
```

## 访问

| 服务 | 地址 |
|------|------|
| UI | http://localhost:3000 |
| Manager API | http://localhost:8080 |
| AgentCenter gRPC | localhost:6751 |

**默认账户**: admin / admin123

## 常用命令

```bash
make proto             # 生成 Protobuf 代码
make build-agent       # 构建 Agent
make build-server      # 构建 Server
make test              # 运行测试
make fmt               # 格式化代码
make lint              # 代码检查
make clean             # 清理构建产物
```

## Git 提交规范

```
<type>: <简短描述>

- 详细改动点
```

**Type**: `feat` | `fix` | `refactor` | `docs` | `test` | `chore`

## 开发流程

1. 选择任务（docs/TODO.md）
2. 开发 + 写测试
3. `make fmt lint test`
4. 提交
