# 社区规范

感谢你对 MxSec Platform 的关注。本文档说明如何参与项目开发。

## 开发环境

### 前置要求

- Go >= 1.21
- Node.js >= 18（前端开发）
- Docker >= 20.10, Docker Compose >= 2.0
- protoc（Protobuf 编译器）
- Make

### 环境搭建

```bash
# 克隆仓库
git clone https://github.com/mxsec/mxsec-platform.git
cd mxsec-platform

# 启动开发环境（带热更新）
make dev-docker-up

# 查看日志
make dev-docker-logs
```

开发环境访问地址：

| 服务 | 地址 |
|------|------|
| Manager API | http://localhost:8080 |
| UI | http://localhost:3000 |
| MySQL | localhost:3306 |

### 常用命令

```bash
make proto           # 生成 Protobuf 代码
make build-agent     # 构建 Agent
make build-server    # 构建 Server
make test            # 运行测试
make fmt             # 格式化代码
make lint            # 代码检查
```

## 代码规范

### Go

- 使用 Zap 结构化日志，禁止 `fmt.Println` / `log.Println`
- 使用统一响应函数（`internal/server/manager/api/response.go`），禁止直接 `c.JSON()`
- 返回错误而非 panic，使用 `fmt.Errorf` 包装错误上下文
- 使用 Preload 避免 N+1 查询，使用事务保证一致性
- 配置从配置文件读取，禁止硬编码
- 测试命名：`TestXxx_描述`，使用 table-driven tests

### TypeScript / Vue

- API 调用统一封装在 `ui/src/api/` 目录，禁止直接调用 axios
- 定义接口类型，使用 TypeScript 严格模式
- 所有 API 调用必须有 try-catch 错误处理
- 组件命名 PascalCase，函数 camelCase，常量 UPPER_CASE

### 通用

- 匹配现有代码风格，不"顺手优化"无关代码
- 不添加超出需求的功能和抽象
- 提交前运行 `make fmt lint test` 确保通过

## 提交流程

### 1. 选择或创建 Issue

在开始编码之前，先确认对应的 Issue 存在。如果是新功能或你发现的 Bug，先创建 Issue 描述清楚需求或问题。

### 2. Fork 并开发

```bash
# Fork 仓库后克隆你的 fork
git clone https://github.com/<your-username>/mxsec-platform.git
cd mxsec-platform

# 创建功能分支
git checkout -b feat/your-feature

# 开发并测试
make test
make lint
```

### 3. 提交代码

Commit 信息格式：

```
<type>: <简短描述>

- 详细改动点1
- 详细改动点2
```

Type 类型：

| Type | 说明 |
|------|------|
| feat | 新功能 |
| fix | Bug 修复 |
| refactor | 重构（不改变外部行为） |
| docs | 文档变更 |
| test | 测试相关 |
| chore | 构建、依赖、配置等 |

### 4. 提交 Pull Request

- PR 标题简洁明了，描述清楚做了什么以及为什么
- 关联对应的 Issue（`Closes #123`）
- 确保 CI 通过
- 新增功能包含对应测试
- 如有 API 变更，更新对应文档

### 5. 代码审查

- 至少一名 Committer 或 Maintainer 审查通过
- 根据审查意见修改后更新 PR
- 审查通过后由 Committer 合并

## 测试要求

- 核心路径覆盖率 >= 85%，整体 >= 70%
- Bug 修复必须附带回归测试
- 集成测试位于 `tests/` 目录

运行测试：

```bash
# 单元测试
go test ./... -v

# 带覆盖率
go test ./... -cover

# 指定包
go test ./internal/server/manager/... -v
```

## Issue 规范

### Bug 报告

提交 Bug 报告时请包含：

- 环境信息（OS、Go 版本、Docker 版本）
- 复现步骤
- 预期行为 vs 实际行为
- 相关日志（脱敏后）

### 功能建议

提交功能建议时请说明：

- 使用场景和动机
- 期望的行为
- 是否愿意参与实现

## 插件开发

如需开发自定义插件，参考现有插件结构：

```
plugins/<plugin-name>/
├── main.go          # 入口
├── engine/          # 核心逻辑
└── go.mod           # 独立 module
```

插件通过 `os.Pipe` + Protobuf 与 Agent 通信，使用 `plugins/lib/go/` 提供的 SDK。

## 沟通渠道

- **Issue**: Bug 报告和功能建议
- **Discussion**: 技术讨论和问题咨询
- **PR**: 代码贡献

感谢每一位贡献者。
