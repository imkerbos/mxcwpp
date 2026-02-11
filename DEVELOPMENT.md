# 开发环境指南

本文档说明如何在本地开发环境中启动和调试 Matrix Cloud Security Platform。

## 前置要求

### 必需依赖

1. **Go** (>= 1.21)
   ```bash
   go version
   ```

2. **Node.js** (>= 16) 和 **npm**
   ```bash
   node --version
   npm --version
   ```

3. **MySQL** (>= 8.0)
   - 使用本地MySQL：127.0.0.1:3306, root/123456
   - 或通过 Docker Compose 启动（修改配置文件即可）

### 可选依赖

- **Docker** 和 **Docker Compose**（用于启动 MySQL 等基础服务）

## 快速开始

### 方式一：Docker 开发环境（推荐，避免 macOS 权限问题）

使用 Docker Compose 在 Linux 容器中运行后端，模拟生产环境：

```bash
# 一键启动（后端在 Docker 中，前端在宿主机）
make dev-docker-up
```

**优势**：
- ✅ 避免 macOS 权限问题
- ✅ 模拟 Linux 生产环境
- ✅ 支持代码热重载（Air）
- ✅ 统一开发环境

详细说明请查看：[Docker 开发环境指南](deploy/dev/README.md)

### 方式二：宿主机开发环境

直接在 macOS 宿主机上运行：

```bash
# 1. 确保MySQL已启动（使用本地MySQL：127.0.0.1:3306, root/123456）
# 2. 初始化数据库（首次运行）
make init-db

# 3. 启动后端和前端
make dev-start
```

### 方式三：分步启动

#### 1. 确保MySQL已启动

```bash
# 使用本地MySQL（127.0.0.1:3306, root/123456）
# 确保MySQL服务已启动

# 初始化数据库（首次运行，如果数据库不存在）
make init-db
```

#### 2. 生成证书（首次运行）

```bash
make certs
```

证书会生成到 `deploy/dev/certs/` 目录。

#### 3. 启动后端 Manager

```bash
# 方式1：使用 Makefile
make dev-server

# 方式2：手动启动
make build-server
./dist/server/manager -config configs/server.yaml
```

后端服务将运行在 `http://localhost:8080`

#### 4. 启动前端 UI

```bash
# 方式1：使用 Makefile
make dev-ui

# 方式2：手动启动
cd ui
npm install  # 首次运行需要安装依赖
npm run dev
```

前端服务将运行在 `http://localhost:3000`

## 配置文件

### 后端配置

配置文件位置：`configs/server.yaml`

主要配置项：
- **数据库连接**：连接到本地 MySQL（127.0.0.1:3306, root/123456）
- **HTTP端口**：8080
- **日志级别**：开发环境使用 `debug` 级别，`console` 格式

如需修改数据库连接信息，编辑 `configs/server.yaml` 文件。

### 前端配置

前端通过 Vite 代理连接到后端：
- 前端端口：3000
- API代理：`/api` -> `http://localhost:8080`

配置文件：`ui/vite.config.ts`

## 数据库初始化

### 自动初始化

Manager 启动时会自动：
1. 执行数据库迁移（创建表结构）
2. 初始化默认策略和规则（如果数据库为空）

默认策略文件位置：`plugins/baseline/config/examples/`

### 手动初始化

如果需要手动初始化数据库：

```bash
# 连接到MySQL
mysql -h localhost -u mxsec_user -pmxsec_password mxsec

# 查看表结构
SHOW TABLES;

# 查看策略
SELECT * FROM policies;
SELECT * FROM rules;
```

## 开发调试

### 后端调试

1. **查看日志**
   - 开发环境日志输出到控制台（console格式）
   - 生产环境日志输出到文件（JSON格式）

2. **API测试**
   ```bash
   # 健康检查
   curl http://localhost:8080/health
   
   # 登录（获取token）
   curl -X POST http://localhost:8080/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{"username":"admin","password":"admin"}'
   ```

3. **使用Postman/Insomnia**
   - Base URL: `http://localhost:8080/api/v1`
   - 认证：Bearer Token（从登录接口获取）

### 前端调试

1. **热重载**
   - Vite 支持热模块替换（HMR）
   - 修改代码后自动刷新

2. **浏览器开发者工具**
   - 打开 `http://localhost:3000`
   - 使用浏览器 DevTools 查看网络请求和日志

3. **API调试**
   - 前端API调用通过 `/api` 代理到后端
   - 可以在浏览器 Network 面板查看请求详情

## 常见问题

### 1. MySQL连接失败

**问题**：`连接 MySQL 失败: dial tcp 127.0.0.1:3306: connect: connection refused`

**解决**：
```bash
# 检查MySQL是否运行
mysql -h 127.0.0.1 -P 3306 -u root -p123456 -e "SELECT 1;"

# 如果连接失败，请确保MySQL服务已启动
# macOS: brew services start mysql
# Linux: sudo systemctl start mysql
# 或使用Docker: docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=123456 mysql:8.0
```

### 2. 证书文件不存在

**问题**：`open deploy/dev/certs/ca.crt: no such file or directory`

**解决**：
```bash
make certs
```

### 3. 端口被占用

**问题**：`bind: address already in use`

**解决**：
```bash
# 查找占用端口的进程
lsof -i :8080  # 后端端口
lsof -i :3000  # 前端端口

# 停止进程或修改配置文件中的端口
```

### 4. UI依赖未安装

**问题**：`Cannot find module 'xxx'`

**解决**：
```bash
cd ui
npm install
```

### 5. 数据库迁移失败

**问题**：数据库表已存在或结构不匹配

**解决**：
```bash
# 删除数据库重新创建（谨慎操作）
mysql -h 127.0.0.1 -P 3306 -u root -p123456 -e "DROP DATABASE IF EXISTS mxsec; CREATE DATABASE mxsec CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"

# 重新启动Manager，会自动执行迁移
# 或使用初始化脚本
make init-db
```

## 停止服务

### 停止开发服务

```bash
# 如果使用 make dev-start 启动，按 Ctrl+C 停止

# 停止Docker服务
make dev-down
```

### 清理资源

```bash
# 停止并删除Docker容器和卷
make docker-clean
```

## 下一步

- 查看 [API文档](docs/design/server-api.md)
- 查看 [插件开发指南](docs/development/plugin-development.md)
- 查看 [TODO列表](docs/TODO.md)
