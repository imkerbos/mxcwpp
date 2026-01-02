# 依赖说明

本文档说明 Matrix Cloud Security Platform 的可选依赖和如何安装。

## 必需依赖

所有依赖已在 `go.mod` 中定义，运行 `go mod download` 即可安装。

## 可选依赖

### Prometheus 客户端库

如果使用 Prometheus 指标导出功能，需要添加以下依赖：

```bash
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
```

或者直接运行：

```bash
go get github.com/prometheus/client_golang/prometheus/promhttp
```

这会自动安装所有相关依赖。

### Redis 客户端库（可选）

如果使用 Redis 缓存功能，需要实现 `RedisClient` 接口。可以使用以下 Redis 客户端库：

- `github.com/redis/go-redis/v9` (推荐)
- `github.com/go-redis/redis/v8`

示例实现：

```go
import (
    "context"
    "time"
    "github.com/redis/go-redis/v9"
    "github.com/imkerbos/mxsec-platform/internal/server/manager/biz"
)

type RedisClientImpl struct {
    client *redis.Client
}

func NewRedisClient(addr string) *RedisClientImpl {
    return &RedisClientImpl{
        client: redis.NewClient(&redis.Options{
            Addr: addr,
        }),
    }
}

func (c *RedisClientImpl) Get(ctx context.Context, key string) (string, error) {
    return c.client.Get(ctx, key).Result()
}

func (c *RedisClientImpl) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
    return c.client.Set(ctx, key, value, expiration).Err()
}

func (c *RedisClientImpl) Del(ctx context.Context, keys ...string) error {
    return c.client.Del(ctx, keys...).Err()
}

func (c *RedisClientImpl) Exists(ctx context.Context, key string) (bool, error) {
    count, err := c.client.Exists(ctx, key).Result()
    return count > 0, err
}
```

## 安装所有依赖

```bash
# 安装必需依赖
go mod download

# 安装 Prometheus 依赖（如果使用指标导出）
go get github.com/prometheus/client_golang/prometheus/promhttp

# 安装 Redis 依赖（如果使用 Redis 缓存）
go get github.com/redis/go-redis/v9
```

## 验证安装

```bash
# 检查依赖
go mod verify

# 查看依赖树
go mod graph
```
