# LLMProxy 服务设计

> **服务定位**：LLMProxy 是 mxsec 六微服务之一，作为**统一的多 LLM 厂商适配网关**，对外屏蔽厂商差异，对内提供"路由 / 缓存 / 计费 / Fallback / 审计 / 隐私脱敏"一体化能力。
>
> **核心约束**：
> 1. mxsec 是工业级开源 CWPP，专精 **Linux 主机 + Kubernetes 容器**；
> 2. mxsec 实时检测靠**本地 ML（ONNX Runtime CPU 推理）**，LLM 仅做语义增强，**默认 `llm.enabled=false`**；
> 3. LLMProxy 是**可选组件**，开启后 Engine / Manager 通过 gRPC mTLS 调用；
> 4. **默认监听模式（observe）** 不阻断业务，LLM 调用也遵循"超额停用而非降级业务"原则；
> 5. **多租户 from-day-1**，所有调用走租户级配额、缓存隔离、审计入账。
>
> **参考**：[`architecture.md`](architecture.md) §2.6 / [`operating-modes.md`](operating-modes.md) / [`multi-tenant.md`](multi-tenant.md) §8 / [`ml-models.md`](ml-models.md)

---

## 1. 设计目标

| 目标 | 说明 |
|------|------|
| 厂商解耦 | 上层业务（Engine / Manager）只看到 `LLMClient` 抽象接口，**永不感知**底层是 OpenAI / Claude / Qwen / 本地 Ollama |
| 场景路由 | 4 大核心场景（告警解释 / Storyline 总结 / NL→Query / 规则起草）按"成本×能力"自动选不同厂商 + 模型 |
| 成本可控 | 租户级月度 USD 配额 + 单次 token cap + Redis 24h 入参缓存 + Embedding 缓存（命中率目标 ≥ 60%） |
| 高可用 | 主厂商失败 3 次进黑名单 5 分钟，按优先级链 fallback；全部失败则降级返回"规则兜底文案" |
| 离网友好 | 自动检测出网失败，切换到本地 Ollama / vLLM；信创/金融客户可强制 `air_gapped=true` 永不出网 |
| 数据隐私 | 入参可配置脱敏（IP / hostname / path / username / payload），出网前 mask；审计原文密文存储 |
| 多租户 | 每租户独立 API Key 池、独立配额、独立缓存命名空间、独立审计流 |
| 合规审计 | 每次调用入 `mxsec.llm.audit` Topic（保留 90d），含 tenant / scene / model / token / cost / cache_hit / fallback_depth |

---

## 2. 在六微服务中的位置

LLMProxy 位于控制面，与 Manager / AgentCenter / Consumer / Engine / VulnSync **同级**，**对内 gRPC mTLS、对外 HTTPS**。

```
                       +-----------------------+
                       |   Manager / Engine    |  <- 业务侧调用方
                       +-----------+-----------+
                                   | gRPC mTLS + Bearer
                                   v
                       +-----------+-----------+
                       |       LLMProxy        |
                       |  - Router (scene→provider)
                       |  - Cache (Redis 24h)  |
                       |  - Quota (tenant)     |
                       |  - Sanitizer (PII)    |
                       |  - Fallback chain     |
                       |  - Audit producer     |
                       +-----+---------+-------+
                             |         |
              +--------------+         +------------------+
              | HTTPS                                    | gRPC/HTTP localhost
              v                                          v
   +----------+-----------+                   +----------+----------+
   | 公网 LLM             |                   | 本地 LLM (离网/信创) |
   | - OpenAI            |                   | - Ollama (Qwen2.5)  |
   | - Anthropic         |                   | - vLLM (Qwen / Llama)|
   | - Google Gemini     |                   | - Xinference        |
   | - 阿里 DashScope    |                   |                     |
   | - DeepSeek/Kimi/智谱|                   |                     |
   | - 火山方舟/硅基流动 |                   |                     |
   +---------------------+                   +---------------------+

                                  审计/计费
                                     |
                                     v
                          +----------+----------+
                          | Kafka                |
                          | mxsec.llm.audit      | -> Consumer -> MySQL/CK
                          +----------------------+
```

服务入口：`cmd/server/llmproxy/main.go`
代码包：`internal/server/llmproxy/`
副本：N 副本无状态，前置 L7 LB；与 Manager / Engine 通过 mTLS 互信（CA 由 `scripts/generate-certs.sh` 签发）。

---

## 3. 与现状对比（升级目标）

当前 `internal/server/manager/biz/llm_assist.go` 是 v1.x 时期内嵌在 Manager 的单文件实现，存在以下硬伤，**全部由 LLMProxy 解决**：

| 当前问题 | LLMProxy 解决方案 |
|---------|------------------|
| 厂商写死 + 字段拼接（半 Claude 半 OpenAI） | Provider 抽象接口 + 各厂商独立 driver |
| 仅 `AnalyzeAlert` 单场景 | 4 大场景路由（alert_explain / storyline_summary / nl2query / rule_draft） |
| 无缓存 | Redis SHA256 缓存 24h，cache_hit 直接返回 |
| 无配额 | 租户级月度 USD + 单次 token cap |
| 无 fallback | 失败链 + 黑名单 + 兜底文案 |
| 无审计 | 每次调用入 `mxsec.llm.audit` Kafka Topic |
| 无脱敏 | 入参 PII / Secret mask |
| 嵌在 Manager（耦合业务 API） | 独立微服务，gRPC 调用 |

升级后 `biz/llm_assist.go` 仅保留**1 个 gRPC 客户端封装**（10 行代码），业务侧调用形如：

```go
res, err := llmClient.AlertExplain(ctx, &llmpb.AlertExplainReq{
    TenantId: tenantID,
    AlertId:  alertID,
    Alert:    alertPB,
})
```

---

## 4. 支持厂商清单

按"调用协议"分两类。所有 OpenAI-Compatible 厂商共用同一份 driver，仅 BaseURL / Model / Key 不同。

### 4.1 原生协议（每家独立 driver）

| 厂商 | 模型 | 用途 | 备注 |
|------|------|------|------|
| OpenAI | `gpt-4o` / `gpt-4o-mini` | 通用高质量 / 廉价分析 | 函数调用最稳，规则起草首选 |
| Anthropic | `claude-3-5-sonnet-20241022` / `claude-3-5-haiku-20241022` / `claude-3-opus-20240229` | Storyline 长链总结 / 高难度推理 | 长上下文 200k 适合攻击链 |
| Google | `gemini-1.5-pro` / `gemini-1.5-flash` | 备选 / 多模态（截图分析） | 价格中等 |

### 4.2 OpenAI-Compatible（统一 driver）

| 厂商 | 推荐模型 | BaseURL（示例） | 用途 |
|------|---------|----------------|------|
| 阿里 DashScope | `qwen-max` / `qwen-plus` / `qwen-turbo` | `https://dashscope.aliyuncs.com/compatible-mode/v1` | 中文场景 / 信创 / 出海合规 |
| DeepSeek | `deepseek-chat` / `deepseek-reasoner` | `https://api.deepseek.com/v1` | **规则起草首推**（推理强 + 便宜） |
| 月之暗面 Kimi | `moonshot-v1-32k` / `moonshot-v1-128k` | `https://api.moonshot.cn/v1` | 长上下文 Storyline |
| 智谱 GLM | `glm-4-plus` / `glm-4-flash` | `https://open.bigmodel.cn/api/paas/v4` | 信创首选 |
| 火山方舟 | `doubao-pro-32k` / `doubao-lite-32k` | `https://ark.cn-beijing.volces.com/api/v3` | 字节系企业 |
| 硅基流动 | `Qwen/Qwen2.5-72B-Instruct` 等 | `https://api.siliconflow.cn/v1` | 多模型聚合，价格压舱 |
| 本地 Ollama | `qwen2.5:7b` / `qwen2.5:14b` / `llama3.1:8b` | `http://ollama:11434/v1` | **离网/信创推荐**，CPU 也能跑 |
| 本地 vLLM | `Qwen2.5-7B-Instruct-AWQ` / `Qwen2.5-14B` | `http://vllm:8000/v1` | 离网 + GPU 高吞吐 |
| 本地 Xinference | 任意 | `http://xinference:9997/v1` | 多模型统一管理 |

> **离网首推**：Ollama + Qwen 2.5-7B-Instruct（int4 量化版 4.4GB，单 CPU 8 核可跑 ~10 tok/s，足以覆盖 4 大场景）。
> **嵌入向量**：本地 BGE-M3（多语种 1024 维）/ MiniLM-L6（384 维），由 Engine 直接调用 ONNX，**LLMProxy 不承担本地嵌入**，仅代理远程 Embedding API。

---

## 5. Provider 抽象接口

### 5.1 接口定义

`internal/server/llmproxy/provider/types.go`

```go
package provider

import (
    "context"
    "io"
)

// Provider 是所有 LLM 厂商的统一抽象。
// driver 只暴露这 4 个方法，路由器、缓存、计费、审计全部在上层完成。
type Provider interface {
    // Name 返回 driver 名称（openai / anthropic / dashscope / openai_compat / ollama ...）
    Name() string

    // Complete 同步补全（最常用，4 大场景都走这个）
    Complete(ctx context.Context, req *CompleteRequest) (*CompleteResponse, error)

    // Stream 流式补全（Storyline 总结、规则起草，UI 增量渲染）
    Stream(ctx context.Context, req *CompleteRequest) (StreamReader, error)

    // Embed 文本向量化（NL→Query、相似告警去重）
    Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error)

    // Count 估算输入 token 数（用于配额预校验，避免超大请求打爆）
    Count(text string, model string) (int, error)

    // Healthz 健康检查（路由器探活用）
    Healthz(ctx context.Context) error
}

// CompleteRequest 厂商无关的请求结构
type CompleteRequest struct {
    Model       string            // 由路由器解析后填入，如 "gpt-4o-mini"
    Messages    []Message         // role: system/user/assistant
    Temperature float32           // 默认 0.2（安全分析需稳定）
    TopP        float32           // 默认 0.9
    MaxTokens   int               // 默认 1024，规则起草 4096
    JSONMode    bool              // 是否强制 JSON 输出
    Stop        []string          // 终止符
    Metadata    map[string]string // 透传用，driver 可忽略
}

type Message struct {
    Role    string // system / user / assistant
    Content string
}

type CompleteResponse struct {
    Text         string
    TokensIn     int
    TokensOut    int
    Model        string  // 实际使用模型（厂商可能 fallback 到 32k 版本）
    FinishReason string  // stop / length / content_filter
    Raw          []byte  // 原始响应体（审计用，可关闭）
}

// StreamReader 流式 chunk 读取
type StreamReader interface {
    io.Closer
    Next() (chunk string, done bool, err error)
}

type EmbedRequest struct {
    Model string
    Input []string
}

type EmbedResponse struct {
    Vectors  [][]float32
    TokensIn int
}
```

### 5.2 driver 落盘结构

```
internal/server/llmproxy/provider/
  ├── types.go              # 接口
  ├── factory.go            # New(cfg) Provider
  ├── openai/
  │   └── driver.go         # 原生 OpenAI Chat Completions / Embeddings
  ├── anthropic/
  │   └── driver.go         # Messages API
  ├── google/
  │   └── driver.go         # Gemini generateContent
  ├── openai_compat/
  │   └── driver.go         # DashScope / DeepSeek / Kimi / 智谱 / 火山 / 硅基 / Ollama / vLLM 共用
  └── localembed/
      └── driver.go         # 仅 Embed，本地 BGE/MiniLM ONNX 兜底（可选）
```

### 5.3 driver 实现要点（OpenAI 为例）

```go
// internal/server/llmproxy/provider/openai/driver.go (节选)
type Driver struct {
    apiKey  string
    baseURL string
    client  *http.Client
    logger  *zap.Logger
}

func (d *Driver) Name() string { return "openai" }

func (d *Driver) Complete(ctx context.Context, req *provider.CompleteRequest) (*provider.CompleteResponse, error) {
    body := map[string]any{
        "model":       req.Model,
        "messages":    toOpenAIMessages(req.Messages),
        "temperature": req.Temperature,
        "top_p":       req.TopP,
        "max_tokens":  req.MaxTokens,
    }
    if req.JSONMode {
        body["response_format"] = map[string]string{"type": "json_object"}
    }
    // ... POST /v1/chat/completions ...
    // ... 错误分类：429 ratelimit / 5xx upstream / 400 bad request ...
    return &provider.CompleteResponse{
        Text:         choice.Message.Content,
        TokensIn:     usage.PromptTokens,
        TokensOut:    usage.CompletionTokens,
        Model:        resp.Model,
        FinishReason: choice.FinishReason,
    }, nil
}
```

### 5.4 错误分类（决定 fallback 行为）

| 错误类型 | HTTP | 是否计入黑名单 | 是否 fallback |
|---------|------|---------------|---------------|
| `ErrAuth` | 401/403 | 是（配置错误）| 是 |
| `ErrRateLimit` | 429 | 是（连续 3 次）| 是 |
| `ErrUpstream` | 5xx / timeout | 是（连续 3 次）| 是 |
| `ErrBadRequest` | 400 | 否（业务错误）| 否，直接返回 |
| `ErrQuotaExceeded` | 上层判定 | 否 | 否，直接返回 429 |
| `ErrContextLength` | 400 + 特征 | 否 | 是（切换到更长上下文模型）|

---

## 6. 场景路由

### 6.1 4 大核心场景

| 场景 ID | 用途 | 推荐主厂商 → 推荐模型 | 默认 fallback 链 | 温度 / max_tokens |
|---------|------|---------------------|------------------|-------------------|
| `alert_explain` | 单告警人话解释 + 处置建议 + MITRE 映射 | `openai` → `gpt-4o-mini` | `dashscope/qwen-turbo` → `ollama/qwen2.5:7b` | 0.2 / 800 |
| `storyline_summary` | 攻击链总结（多事件 → 一段话）| `anthropic` → `claude-3-5-sonnet` | `kimi/moonshot-v1-32k` → `dashscope/qwen-plus` → `ollama/qwen2.5:14b` | 0.3 / 1500 |
| `nl2query` | 自然语言转告警查询 DSL（CEL / SQL） | `deepseek` → `deepseek-chat` | `openai/gpt-4o-mini` → `dashscope/qwen-plus` → `ollama/qwen2.5:7b` | 0.0 / 600 |
| `rule_draft` | 检测规则草拟（CEL / Sigma / Falco） | `deepseek` → `deepseek-reasoner` | `openai/gpt-4o` → `anthropic/claude-3-5-sonnet` → `ollama/qwen2.5:14b` | 0.1 / 4096 |
| `embedding` | 相似告警去重 / 知识检索 | `openai` → `text-embedding-3-small` | `dashscope/text-embedding-v3` → `local/bge-m3` | — |

> **选型依据**：
> - GPT-4o-mini：单次成本 ~$0.0002，告警解释最便宜稳定；
> - Claude 3.5 Sonnet：长上下文 + 因果推理强，Storyline 总结无对手；
> - DeepSeek Reasoner：国产推理模型性价比之王，CEL/Sigma 规则起草命中率 > 同价位 5 倍；
> - Qwen2.5（远程 + 本地）：信创 / 离网必备兜底。

### 6.2 路由器实现

`internal/server/llmproxy/router/router.go`

```go
type Router struct {
    cfg      *Config
    registry map[string]provider.Provider // driver 实例池，key=providerName
    blacklist *Blacklist                  // 黑名单（5 分钟 TTL）
    logger   *zap.Logger
}

// Route 按场景 + 租户配置解析出 ordered fallback chain
func (r *Router) Route(ctx context.Context, scene string, tenantID string) ([]Hop, error) {
    // 1) 加载租户级覆盖（tenants.llm_provider JSON）
    tCfg := r.loadTenantCfg(tenantID)

    // 2) 取场景的 primary + fallbacks
    sc := r.cfg.Scenes[scene]
    chain := append([]Hop{sc.Primary}, sc.Fallbacks...)

    // 3) 租户级覆盖（可指定 primary 强制本地、可禁用某厂商）
    chain = applyTenantOverride(chain, tCfg)

    // 4) 离网模式：剔除所有 public provider
    if r.cfg.AirGapped || tCfg.AirGapped {
        chain = filterLocalOnly(chain)
    }

    // 5) 黑名单过滤
    chain = r.blacklist.Filter(chain)

    if len(chain) == 0 {
        return nil, ErrNoAvailableProvider
    }
    return chain, nil
}

type Hop struct {
    Provider string  // openai / anthropic / openai_compat:deepseek / ollama ...
    Model    string  // gpt-4o-mini
    BaseURL  string  // 仅 openai_compat 用
}
```

### 6.3 离网检测

`internal/server/llmproxy/router/online.go` 后台 goroutine 每 30s 探活 `https://1.1.1.1` 与每个公网 provider 的 `/healthz`：

```go
func (o *OnlineDetector) Loop() {
    t := time.NewTicker(30 * time.Second)
    for range t.C {
        ok := o.probe("https://1.1.1.1") || o.probe("https://www.aliyun.com")
        o.online.Store(ok)
        if !ok && o.cfg.AutoDowngrade {
            o.logger.Warn("出网检测失败，自动降级到本地 provider")
            // 路由器读取 o.online.Load()，自动 filterLocalOnly
        }
    }
}
```

`air_gapped: true` 配置则**永不探活、永不出网**（信创/金融 KA 客户硬约束）。

---

## 7. 缓存层（Redis 24h）

### 7.1 缓存键设计

```
mxsec:llm:cache:{tenant_id}:{scene}:{sha256(model+messages+temperature+top_p+max_tokens+json_mode)}
mxsec:ml:embedding:{tenant_id}:{model}:{sha256(input)}
```

| 关键点 | 说明 |
|--------|------|
| TTL | 默认 24h，租户可改（最长 7d） |
| 命名空间 | 按租户隔离，**严禁跨租户复用**（即便 prompt 相同） |
| Hash 因子 | 全部"会影响输出确定性"的字段都参与 hash（model / messages / temperature / top_p / max_tokens / json_mode） |
| 序列化 | JSON gzip 压缩入 Redis（单 key 上限 256KB，超过则不缓存） |
| 命中统计 | Prometheus `mxsec_llm_cache_hits_total{tenant, scene}` |
| 命中即返回 | 不消耗厂商配额，但仍写一条审计（`cache_hit=true`，cost=0） |

### 7.2 缓存策略

| 场景 | 是否缓存 | 原因 |
|------|---------|------|
| `alert_explain` | ✅ | 同告警 24h 内重复查询常见 |
| `storyline_summary` | ✅ | 攻击链短期内不变 |
| `nl2query` | ✅ | 同一句自然语言不必每次问 |
| `rule_draft` | ❌ | 用户每次都要新草稿，缓存反伤体验 |
| `embedding` | ✅ | 同字符串永远同向量 |
| `stream` 模式 | ❌ | 流式不缓存（首个 token 已返回） |

### 7.3 失效场景

- 用户主动点击「重新生成」→ Header `X-LLM-Bypass-Cache: 1`，强制透传；
- 模型升级（如 `gpt-4o-mini` → `gpt-4o-mini-2024-08-06`）→ hash 自然变化；
- 租户切换 `llm_provider` → 旧缓存自然失效（key 含 model）。

---

## 8. Fallback 策略

### 8.1 单次调用流程

```
RouterChain = [primary, fb1, fb2, ..., fbN]   N 通常 ≤ 4
for hop in chain:
    if hop in blacklist:   continue
    if provider.healthz fail: blacklist += hop; continue
    try:
        resp = provider.Complete(req with hop.Model)
        return resp                  # 成功，记 cost / token / fallback_depth=i
    except ErrAuth | ErrRateLimit | ErrUpstream:
        recordFailure(hop)           # 滑动窗口 5 分钟内 ≥3 次进黑名单
        continue
    except ErrBadRequest | ErrContextLength:
        # 业务错误不重试（除非 ErrContextLength + 链中有更长 ctx 模型）
        if ErrContextLength and hasLongerCtxInChain:
            continue
        raise

raise ErrAllProvidersFailed          # 触发兜底文案
```

### 8.2 黑名单

`mxsec:llm:provider:blacklist:{provider_name}` Redis Set，TTL 5min。
失败计数：`mxsec:llm:provider:failcount:{provider}` 自增，1 分钟滑窗 ≥ 3 → 加黑。

### 8.3 兜底文案（last resort）

所有 provider 都失败时，**不抛错给 UI**，而是返回结构化兜底响应，业务侧可正常展示：

```json
{
  "summary": "LLM 服务当前不可用，已回退到规则解释：该告警由 BRUTE_FORCE_SSH 规则触发，5 分钟内同源 IP 失败登录 ≥ 5 次。",
  "risk_level": "high",
  "attack_vector": "unknown",
  "recommendations": ["封禁源 IP（在 protect 模式下自动执行）", "检查目标用户认证日志"],
  "mitre_mapping": ["T1110.001"],
  "_meta": {"fallback": "rule_based", "providers_failed": ["openai", "dashscope", "ollama"]}
}
```

兜底文案模板由 `internal/server/llmproxy/fallback/templates.go` 维护，与 Engine 规则 ID 一一对应。

---

## 9. 租户级配额

> 与 [`multi-tenant.md`](multi-tenant.md) §8 严格对齐：每租户 `quota_llm_usd` 字段已写在 `tenants` 表。

### 9.1 配额维度

| 维度 | 字段 | 默认 | 行为 |
|------|------|------|------|
| 月度成本上限 | `tenants.quota_llm_usd` | $100 | 触顶 → 当月停用 LLM（但仍走兜底文案） |
| 单次最大 token | `tenants.quota_llm_single_tokens_max` | 16384 (in+out) | 超出 → 直接拒绝（ErrBadRequest） |
| QPS 限流 | `tenants.quota_llm_qps` | 5 | 滑窗令牌桶，超出 429 |
| 月度调用次数 | `tenants.quota_llm_calls_per_month` | 100000 | 触顶 → 停用 |
| 告警阈值 | 80% 阈值发邮件 + 95% 站内信 + 100% 强制停用 | — | Manager 通知中心 |

### 9.2 实现

`internal/server/llmproxy/quota/manager.go`

```go
type Manager struct {
    redis  *redis.Client
    db     *gorm.DB
    logger *zap.Logger
}

// PreCheck 调用前校验：估算成本 + 比较剩余配额
func (q *Manager) PreCheck(ctx context.Context, tenantID string, estimatedTokensIn, estimatedTokensOut int, modelPrice ModelPrice) error {
    // 1) 单次 token cap
    if estimatedTokensIn+estimatedTokensOut > q.singleMax(tenantID) {
        return ErrSingleTooLarge
    }
    // 2) QPS 令牌桶
    if !q.qpsBucket(tenantID).Allow() {
        return ErrQPSLimit
    }
    // 3) 月度 USD（Redis HINCRBYFLOAT）
    monthKey := fmt.Sprintf("mxsec:llm:tenant:cost:%s:%s", tenantID, time.Now().Format("200601"))
    current, _ := q.redis.Get(ctx, monthKey).Float64()
    estimatedCost := modelPrice.Estimate(estimatedTokensIn, estimatedTokensOut)
    cap := q.monthlyCap(tenantID)
    if current+estimatedCost > cap {
        q.notifyQuotaExceeded(tenantID, current, cap)
        return ErrMonthlyQuotaExceeded
    }
    return nil
}

// Settle 调用后实际入账（真实 token 数）
func (q *Manager) Settle(ctx context.Context, tenantID string, actualIn, actualOut int, modelPrice ModelPrice) {
    cost := modelPrice.Calc(actualIn, actualOut)
    monthKey := fmt.Sprintf("mxsec:llm:tenant:cost:%s:%s", tenantID, time.Now().Format("200601"))
    q.redis.IncrByFloat(ctx, monthKey, cost)
    q.redis.Expire(ctx, monthKey, 32*24*time.Hour) // 留 32 天给月底对账
    // 阈值告警
    cap := q.monthlyCap(tenantID)
    if newVal := current + cost; crossesThreshold(current, newVal, cap, 0.8) {
        q.notifyThreshold(tenantID, 0.8)
    }
}
```

### 9.3 计费模型表（内嵌，每月手动同步官方价格）

`internal/server/llmproxy/quota/pricing.go` 维护：

```go
var Pricing = map[string]ModelPrice{
    "openai/gpt-4o":           {InputPer1K: 0.0025, OutputPer1K: 0.01},
    "openai/gpt-4o-mini":      {InputPer1K: 0.00015, OutputPer1K: 0.0006},
    "anthropic/claude-3-5-sonnet-20241022": {InputPer1K: 0.003, OutputPer1K: 0.015},
    "anthropic/claude-3-5-haiku-20241022":  {InputPer1K: 0.0008, OutputPer1K: 0.004},
    "google/gemini-1.5-pro":   {InputPer1K: 0.00125, OutputPer1K: 0.005},
    "google/gemini-1.5-flash": {InputPer1K: 0.000075, OutputPer1K: 0.0003},
    "dashscope/qwen-max":      {InputPer1K: 0.0028, OutputPer1K: 0.0084},  // 人民币 USD 估算
    "dashscope/qwen-plus":     {InputPer1K: 0.0011, OutputPer1K: 0.0028},
    "dashscope/qwen-turbo":    {InputPer1K: 0.00028, OutputPer1K: 0.00084},
    "deepseek/deepseek-chat":  {InputPer1K: 0.00014, OutputPer1K: 0.00028},
    "deepseek/deepseek-reasoner": {InputPer1K: 0.00055, OutputPer1K: 0.0022},
    "kimi/moonshot-v1-32k":    {InputPer1K: 0.0017, OutputPer1K: 0.0017},
    "ollama/*":                {InputPer1K: 0, OutputPer1K: 0}, // 本地零成本
    "vllm/*":                  {InputPer1K: 0, OutputPer1K: 0},
}
```

> **价格变动需走 PR**，避免运行时漂移。`mxctl llmproxy pricing-sync` 命令辅助拉官方文档对比。

---

## 10. 数据隐私 / 脱敏

### 10.1 脱敏字段

| 字段类型 | 默认行为 | 可配置 |
|---------|---------|-------|
| IP（v4/v6） | mask 后 8 位（`192.0.2.45` → `192.0.2.XX`） | per-tenant 关闭 |
| hostname | mask 后半段（`prod-db-01.bank-a.com` → `prod-db-XX.***`） | per-tenant 关闭 |
| 文件路径 | 保留 basename + 父目录，前缀 mask（`/data/user/zhang/.ssh/id_rsa` → `/***/***/.ssh/id_rsa`） | per-tenant 关闭 |
| 用户名 | mask（`zhangsan` → `zh***an`） | per-tenant 关闭 |
| payload / 命令行 | 正则匹配 secret 模式（`token=xxx` / `password=xxx` / `AKIA...`）替换 `***` | per-tenant 关闭 |
| process args 中绝对路径 | 同 path 规则 | per-tenant 关闭 |
| tenant_id / host_id | **不脱敏**（出网前会在 prompt 中替换为 `HOST_A` 这类占位符）| 强约束 |

### 10.2 脱敏实现

`internal/server/llmproxy/sanitize/sanitize.go`

```go
type Sanitizer struct {
    cfg *SanitizeConfig
    // 正则池
    ipv4Re     *regexp.Regexp
    ipv6Re     *regexp.Regexp
    pathRe     *regexp.Regexp
    secretsRes []*regexp.Regexp
}

// Apply 在 prompt 拼装后、provider 调用前执行
func (s *Sanitizer) Apply(req *provider.CompleteRequest) (*provider.CompleteRequest, *Mapping) {
    // 1) 抽取出现的 IP / hostname / path，建占位符映射（用于回填可选）
    // 2) 替换原文中的真实值
    // 3) 返回脱敏后的 req + Mapping（审计用，加密存储）
}
```

### 10.3 离网模式跳过脱敏

`air_gapped=true` 且全部 hop 都是本地 provider 时，可配置 `sanitize.on_air_gapped=false` 跳过脱敏（数据不出网，无泄漏风险），换取更高的分析准确度。

---

## 11. 审计与计量

### 11.1 审计 Topic `mxsec.llm.audit`

每次调用（含 cache_hit / fallback / error）都产一条消息。Partitions=3，Retention=90d，Consumer 写 MySQL `llm_audit_log` + ClickHouse `llm_audit_archive`。

```json
{
  "audit_id": "llma-2026060600001",
  "tenant_id": "t-bank-a",
  "user_id": "u-12345",
  "scene": "alert_explain",
  "request_id": "req-abc",
  "alert_id": "alrt-2026060100001",

  "provider_used": "openai",
  "model_used": "gpt-4o-mini",
  "fallback_depth": 0,
  "providers_tried": ["openai"],

  "cache_hit": false,
  "tokens_in": 612,
  "tokens_out": 184,
  "cost_usd": 0.000202,
  "latency_ms": 1843,

  "sanitized": true,
  "air_gapped": false,

  "status": "success",
  "error_class": "",

  "prompt_hash": "sha256:...",
  "response_hash": "sha256:...",
  "ts": "2026-06-06T10:23:45Z"
}
```

> **prompt / response 全文不入 Topic**，仅 hash。如客户开启 `audit.full_payload=true`（合规要求），全文写本地 MinIO/S3 + 客户私钥加密。

### 11.2 Prometheus 指标

```
# 计数
mxsec_llm_calls_total{tenant, scene, provider, model, status, cache_hit}
mxsec_llm_tokens_total{tenant, scene, direction="in|out"}
mxsec_llm_cost_usd_total{tenant, scene, provider}
mxsec_llm_cache_hits_total{tenant, scene}
mxsec_llm_fallback_total{tenant, scene, depth}
mxsec_llm_blacklist_events_total{provider, reason}
mxsec_llm_quota_exceeded_total{tenant, dimension="usd|qps|calls|single_tokens"}

# 直方图
mxsec_llm_latency_seconds{tenant, scene, provider, le}     # 延迟
mxsec_llm_tokens_per_call{scene, le}                       # token 分布

# Gauge
mxsec_llm_provider_healthy{provider}                       # 1/0
mxsec_llm_online{}                                         # 出网状态
```

### 11.3 计量上报到 metering

每日 0:00，LLMProxy 把租户级 `mxsec:llm:tenant:cost:{tenant}:{YYYYMM}` 聚合写入 `mxsec.metering.usage` Topic（见 [`multi-tenant.md`](multi-tenant.md) §8），完成计费打通。

---

## 12. 配置 YAML 示例

`/etc/mxsec/llmproxy.yaml`

```yaml
service:
  listen: ":18900"             # gRPC 端口
  health_listen: ":18901"      # HTTP /healthz /metrics
  mtls:
    cert: /etc/mxsec/certs/llmproxy.crt
    key: /etc/mxsec/certs/llmproxy.key
    ca:  /etc/mxsec/certs/ca.crt
  internal_bearer: ${MXSEC_INTERNAL_BEARER}

# 离网开关：true 则永不出网，禁用所有公网 provider，仅使用本地
air_gapped: false

# 出网自动探测降级
auto_downgrade_on_offline: true

# Redis（缓存 + 配额）
redis:
  addr: redis-sentinel:26379
  master_name: mymaster
  db: 3

# Kafka（审计）
kafka:
  brokers: [kafka-1:9092, kafka-2:9092, kafka-3:9092]
  audit_topic: mxsec.llm.audit
  metering_topic: mxsec.metering.usage

# 缓存策略
cache:
  enabled: true
  ttl: 24h
  max_value_size_bytes: 262144
  bypass_header: "X-LLM-Bypass-Cache"

# 脱敏
sanitize:
  enabled: true
  on_air_gapped: false         # 离网时是否仍脱敏
  rules:
    ip: mask
    hostname: mask
    path: mask
    username: mask
    secrets: mask

# 厂商凭证池（密钥从 K8s Secret / Vault 注入，不入版本库）
providers:
  - name: openai
    driver: openai
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    timeout: 30s

  - name: anthropic
    driver: anthropic
    base_url: https://api.anthropic.com
    api_key: ${ANTHROPIC_API_KEY}
    timeout: 60s

  - name: dashscope
    driver: openai_compat
    base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${DASHSCOPE_API_KEY}
    timeout: 30s

  - name: deepseek
    driver: openai_compat
    base_url: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}
    timeout: 60s

  - name: kimi
    driver: openai_compat
    base_url: https://api.moonshot.cn/v1
    api_key: ${KIMI_API_KEY}
    timeout: 60s

  - name: glm
    driver: openai_compat
    base_url: https://open.bigmodel.cn/api/paas/v4
    api_key: ${GLM_API_KEY}
    timeout: 30s

  - name: ollama
    driver: openai_compat
    base_url: http://ollama:11434/v1
    api_key: "ollama"           # 占位，本地无校验
    timeout: 120s
    is_local: true

  - name: vllm
    driver: openai_compat
    base_url: http://vllm:8000/v1
    api_key: "vllm"
    timeout: 120s
    is_local: true

# 场景路由（全局默认，可被租户覆盖）
scenes:
  alert_explain:
    primary:   { provider: openai,    model: gpt-4o-mini, temperature: 0.2, max_tokens: 800 }
    fallbacks:
      - { provider: dashscope, model: qwen-turbo,    temperature: 0.2, max_tokens: 800 }
      - { provider: ollama,    model: qwen2.5:7b,    temperature: 0.2, max_tokens: 800 }
    json_mode: true

  storyline_summary:
    primary:   { provider: anthropic, model: claude-3-5-sonnet-20241022, temperature: 0.3, max_tokens: 1500 }
    fallbacks:
      - { provider: kimi,      model: moonshot-v1-32k,  temperature: 0.3, max_tokens: 1500 }
      - { provider: dashscope, model: qwen-plus,        temperature: 0.3, max_tokens: 1500 }
      - { provider: ollama,    model: qwen2.5:14b,      temperature: 0.3, max_tokens: 1500 }

  nl2query:
    primary:   { provider: deepseek,  model: deepseek-chat, temperature: 0.0, max_tokens: 600 }
    fallbacks:
      - { provider: openai,    model: gpt-4o-mini,    temperature: 0.0, max_tokens: 600 }
      - { provider: dashscope, model: qwen-plus,      temperature: 0.0, max_tokens: 600 }
      - { provider: ollama,    model: qwen2.5:7b,     temperature: 0.0, max_tokens: 600 }
    json_mode: true

  rule_draft:
    primary:   { provider: deepseek,  model: deepseek-reasoner, temperature: 0.1, max_tokens: 4096 }
    fallbacks:
      - { provider: openai,    model: gpt-4o,                temperature: 0.1, max_tokens: 4096 }
      - { provider: anthropic, model: claude-3-5-sonnet-20241022, temperature: 0.1, max_tokens: 4096 }
      - { provider: ollama,    model: qwen2.5:14b,           temperature: 0.1, max_tokens: 4096 }
    cache: false   # 规则起草不缓存

  embedding:
    primary:   { provider: openai,    model: text-embedding-3-small }
    fallbacks:
      - { provider: dashscope, model: text-embedding-v3 }

# 黑名单
blacklist:
  failure_window: 1m
  failure_threshold: 3
  ttl: 5m

# 默认租户配额（被 tenants 表覆盖）
default_quota:
  monthly_usd: 100.0
  qps: 5
  single_tokens_max: 16384
  calls_per_month: 100000
  threshold_warn: 0.8
  threshold_block: 1.0

# 兜底文案
fallback:
  enabled: true
  template_dir: /etc/mxsec/llmproxy/templates
```

### 12.1 租户级 YAML 覆盖

`tenants.llm_provider` JSON 列示例（KA 客户 t-bank-a 强制本地）：

```json
{
  "air_gapped": true,
  "scenes": {
    "alert_explain":     {"primary_provider": "ollama",  "primary_model": "qwen2.5:14b"},
    "storyline_summary": {"primary_provider": "vllm",    "primary_model": "Qwen2.5-14B-Instruct-AWQ"},
    "rule_draft":        {"primary_provider": "ollama",  "primary_model": "qwen2.5:14b"}
  },
  "disabled_providers": ["openai", "anthropic", "google"],
  "monthly_usd_override": 0,
  "qps_override": 2,
  "sanitize_on_air_gapped": false
}
```

> 加载顺序：全局 yaml → 租户 JSON 覆盖 → 请求 header 覆盖（`X-LLM-Force-Provider`，仅 SystemAdmin 可用）。

---

## 13. gRPC API（业务侧调用）

`api/proto/llmproxy/v1/llmproxy.proto`（节选）

```proto
syntax = "proto3";
package mxsec.llmproxy.v1;

service LLMProxy {
  // 场景化便捷接口（推荐）
  rpc AlertExplain      (AlertExplainReq)      returns (AlertExplainResp);
  rpc StorylineSummary  (StorylineSummaryReq)  returns (StorylineSummaryResp);
  rpc NL2Query          (NL2QueryReq)          returns (NL2QueryResp);
  rpc RuleDraft         (RuleDraftReq)         returns (stream RuleDraftChunk);

  // 通用接口（高级用户）
  rpc Complete          (CompleteReq)          returns (CompleteResp);
  rpc Stream            (CompleteReq)          returns (stream CompleteChunk);
  rpc Embed             (EmbedReq)             returns (EmbedResp);

  // 运维
  rpc Healthz           (HealthzReq)           returns (HealthzResp);
  rpc TenantUsage       (TenantUsageReq)       returns (TenantUsageResp);
}

message AlertExplainReq {
  string tenant_id = 1;
  string user_id   = 2;
  string alert_id  = 3;
  Alert  alert     = 4;       // 结构化告警，避免上游再拼 prompt
  bool   bypass_cache = 5;
}

message AlertExplainResp {
  string summary           = 1;
  string risk_level        = 2;       // critical/high/medium/low
  string attack_vector     = 3;
  repeated string recommendations = 4;
  repeated string mitre_mapping   = 5;
  CallMeta meta            = 6;
}

message CallMeta {
  string provider_used    = 1;
  string model_used       = 2;
  int32  fallback_depth   = 3;
  bool   cache_hit        = 4;
  int32  tokens_in        = 5;
  int32  tokens_out       = 6;
  double cost_usd         = 7;
  int64  latency_ms       = 8;
  bool   used_fallback_template = 9;
}
```

### 13.1 业务侧调用示例

`internal/server/manager/biz/llm_assist_v2.go`（升级后）

```go
type LLMAssistV2 struct {
    client llmpbv1.LLMProxyClient   // gRPC 客户端
    logger *zap.Logger
}

func (l *LLMAssistV2) ExplainAlert(ctx context.Context, alert *model.Alert) (*AnalysisResult, error) {
    tenantID := tenant.FromContext(ctx)
    userID, _ := auth.UserIDFromContext(ctx)

    resp, err := l.client.AlertExplain(ctx, &llmpbv1.AlertExplainReq{
        TenantId: tenantID,
        UserId:   userID,
        AlertId:  fmt.Sprintf("%d", alert.ID),
        Alert:    toAlertPB(alert),
    })
    if err != nil {
        // gRPC 已被 LLMProxy 兜底，仅在 ErrQuotaExceeded / ErrBadRequest 时才报错
        if status.Code(err) == codes.ResourceExhausted {
            return nil, response.ErrLLMQuotaExceeded
        }
        return nil, fmt.Errorf("LLM proxy: %w", err)
    }
    return &AnalysisResult{
        Summary:         resp.Summary,
        RiskLevel:       resp.RiskLevel,
        AttackVector:    resp.AttackVector,
        Recommendations: resp.Recommendations,
        MitreMapping:    resp.MitreMapping,
    }, nil
}
```

---

## 14. 调用流程图

### 14.1 单次场景调用全链路

```
[Engine / Manager]
     │  gRPC AlertExplain(tenant_id, alert)
     v
+----+--------------------------------+
|         LLMProxy gRPC Server        |
+-------------------------------------+
     │
     v
[1] mTLS + Bearer 验证 + 反序列化
     │
     v
[2] 加载租户配置 (tenants.llm_provider JSON + cfg.scenes)
     │
     v
[3] QuotaManager.PreCheck
     ├── 单次 token cap 超限 -> 直接返回 ErrSingleTooLarge
     ├── QPS 令牌桶失败       -> 返回 ErrQPSLimit
     └── 月度 USD 超限         -> 返回 ErrMonthlyQuotaExceeded + 兜底文案
     │
     v
[4] Sanitizer.Apply（脱敏，air_gapped 模式可关）
     │
     v
[5] Cache.Get(tenant, scene, hash(req))
     ├── hit  -> Audit(cache_hit=true, cost=0) -> 返回缓存
     └── miss -> 继续
     │
     v
[6] Router.Route(scene, tenant_id) -> chain = [primary, fb1, fb2, ...]
     │   (air_gapped / 黑名单 / 出网探活 三重过滤)
     v
[7] for hop in chain:
        provider := registry[hop.Provider]
        if !provider.Healthz() { blacklist; continue }
        resp, err := provider.Complete(req with hop.Model)
        if err == retryable:
            failureRecord(hop)
            continue
        if err == business:
            break (don't fallback)
        success -> goto [8]
     │
     v
[8] QuotaManager.Settle(tokens_in, tokens_out, cost)
     │
     v
[9] Cache.Set(tenant, scene, hash) TTL=24h
     │
     v
[10] Audit -> Kafka mxsec.llm.audit
     │   (含 fallback_depth / providers_tried / cost)
     v
[11] 返回 AlertExplainResp(+CallMeta)
     │
     v
[Engine / Manager]
```

### 14.2 流式（rule_draft）调用

```
Client                 LLMProxy                  Provider
  │ gRPC Stream(req)        │                         │
  ├────────────────────────>│ Quota.PreCheck          │
  │                         │ Sanitize                │
  │                         │ Cache.miss (不缓存)     │
  │                         │ Router.Route            │
  │                         │ provider.Stream()       │
  │                         ├────────────────────────>│
  │                         │                         │ chunk1
  │ <───────────────────────┤ <── chunk1              │
  │                         │                         │ chunk2
  │ <───────────────────────┤ <── chunk2              │
  │ ...                     │                         │
  │                         │  done                   │
  │                         │ Quota.Settle(actual)    │
  │                         │ Audit                   │
  │ <── EOF                 │                         │
```

### 14.3 离网降级

```
T0    online_detector probe 1.1.1.1   ─── fail
       ├── online.Store(false)
       └── log.Warn("offline, auto downgrade")

T0+1s 业务侧调用 AlertExplain
       Router.Route:
         chain (from cfg) = [openai, dashscope, ollama]
         filterLocalOnly  = [ollama]            (因 online=false)
         blacklist        = [ollama]
       provider=ollama 调用成功 -> 返回
```

---

## 15. 4 大场景接入细节

### 15.1 告警解释（`alert_explain`）

**调用方**：Manager UI 详情页点击「AI 解释」按钮（同步）+ Engine 高危告警自动触发（异步）

**Prompt 模板**（system + user 分离，便于 cache hash 稳定）：

```
[system]
你是一名工业级 CWPP 平台的安全分析师。基于结构化告警 JSON，输出 JSON 对象，
字段：summary（中文 2-3 句）、risk_level（critical/high/medium/low）、
attack_vector、recommendations（数组）、mitre_mapping（数组）。
仅返回 JSON，不要 markdown 围栏。

[user]
告警：
{脱敏后的 alert JSON}
```

**关键点**：JSON Mode 强制；max_tokens=800；缓存 24h；命中率目标 ≥ 50%。

### 15.2 攻击链总结（`storyline_summary`）

**调用方**：Engine 产出 Storyline 时异步调用；Manager 详情页 SSR 渲染时按需调用（带缓存）。

**输入**：N 条事件按时间线（脱敏后）。
**输出**：一段中文叙事 + ATT&CK 战术链 + 关键 IOC 列表。

**关键点**：长上下文优先 Claude / Kimi；单次输入可达 30k tokens；max_tokens=1500；缓存 24h。

### 15.3 自然语言转查询（`nl2query`）

**调用方**：Manager 全局搜索框「问 AI」入口。

**输入**：用户自然语言 + 当前 schema 摘要 + few-shot 示例（3 条）。
**输出**：JSON `{ "dsl": "...", "explain": "..." }`，DSL 可为 mxsec CEL 表达式或只读 SQL。

**关键点**：温度 0；JSON Mode；DSL 在执行前必须经过 `internal/server/manager/biz/dsl_validator.go` 白名单校验（拒绝写操作）；缓存 24h。

### 15.4 规则起草（`rule_draft`）

**调用方**：Manager 规则中心「AI 起草」按钮。

**输入**：用户描述 + 选择目标格式（CEL / Sigma / Falco）+ 平台规则模板片段。
**输出**：完整规则 YAML / CEL 表达式 + 测试用例 + 误报点提示，**流式返回**便于 UI 增量渲染。

**关键点**：DeepSeek Reasoner 首选；不缓存；max_tokens=4096；UI 强制人工评审后入库。

---

## 16. Engine ↔ LLMProxy 集成约束

Engine 调用 LLMProxy 必须遵守：

1. **不阻塞主检测链路**：LLM 调用走独立 goroutine，超时 5s，失败/超时不影响告警生成；
2. **只增强不决定**：LLM 输出仅写入 `alert.llm_enrichment` 字段（独立列），不影响 `severity` / `would_action` / `action` 判定；
3. **遵守 mode**：observe 模式下 LLM 解释仅入 audit，不触发任何动作类响应；
4. **频控**：Engine 按规则维度做二级限流，避免单租户极端场景把配额一次打空；
5. **降级**：LLM 不可用时 Engine 仍正常出告警，UI 显示「AI 解释暂不可用，已使用规则兜底」。

详见 [`engine-design.md`](engine-design.md) §LLM 增强章节。

---

## 17. 部署形态

### 17.1 默认部署（公网客户）

```
llmproxy:
  image: mxsec/llmproxy:v1.0
  replicas: 2
  env:
    OPENAI_API_KEY: from-secret
    DASHSCOPE_API_KEY: from-secret
  resources:
    cpu: 500m
    mem: 256Mi
```

### 17.2 离网客户（信创 / 金融）

```
llmproxy:
  config: { air_gapped: true }

ollama:
  image: ollama/ollama:latest
  replicas: 1
  resources: { cpu: 8, mem: 16Gi }     # 跑 qwen2.5:14b
  volumes:
    - /data/ollama-models:/root/.ollama

# 启动后预拉模型
# ollama pull qwen2.5:7b
# ollama pull qwen2.5:14b
# ollama pull bge-m3
```

可选用 vLLM + GPU 提高吞吐：

```
vllm:
  image: vllm/vllm-openai:latest
  command: ["--model", "Qwen/Qwen2.5-14B-Instruct-AWQ", "--max-model-len", "32768"]
  resources:
    nvidia.com/gpu: 1
    mem: 32Gi
```

### 17.3 多副本扩缩

LLMProxy 无状态，按 QPS 水平扩缩；Redis（缓存 + 配额）共享，Kafka 共享。HPA 触发指标：`mxsec_llm_calls_total` rate + CPU。

---

## 18. SLO 与容量

| 指标 | 目标 |
|------|------|
| `Complete` P95 延迟（cache_hit） | ≤ 10ms |
| `Complete` P95 延迟（cache_miss, 公网 provider） | ≤ 5s |
| `Complete` P95 延迟（cache_miss, ollama 7b CPU） | ≤ 30s |
| 可用性 | 99.9%（含 fallback） |
| 缓存命中率（`alert_explain`） | ≥ 50% |
| 离网模式下兜底文案命中率 | 100%（永不让业务侧报错） |
| 每副本 QPS | ≥ 50（cache_hit）/ ≥ 10（公网透传） |
| 单租户月度成本告警准时率 | 95%（80% 阈值触发）/ 100%（100% 阈值触发） |

---

## 19. 测试矩阵

| 测试 | 目标 |
|------|------|
| 厂商切换 | 同 prompt 切到 5 个厂商，输出结构稳定（JSON schema 不破） |
| Fallback 链 | mock primary 5xx，fb1 自动接管；3 次失败后入黑名单 5min |
| 缓存命中 | 相同 req hash 第二次 < 10ms；不同 tenant 同 prompt 缓存隔离 |
| 配额 | 月度 USD 到 80% / 95% / 100% 三档触发预期行为 |
| 单次 token cap | 超大 prompt 直接 ErrSingleTooLarge，不打 provider |
| QPS 限流 | 同租户 6 QPS 持续 1 分钟，4xx ≥ 1/6 |
| 离网 | 拔网 + air_gapped=true，链路自动走 ollama，无 5xx |
| 脱敏 | mock prompt 含 IP/path/secret，出网请求体中已 mask |
| 跨租户 | t-A 不能看 t-B 的 audit_log / 缓存 / 配额 |
| 兜底文案 | mock 全 provider 失败，UI 收到结构化 fallback，不抛 5xx |
| 流式 | rule_draft 1500 tokens 输出，chunk 顺序无错乱，断流可重连（Stream 端 reconnect） |
| 配置热更新 | tenants.llm_provider 改后 30s 内生效，无需重启 |

---

## 20. 安全考量

| 风险 | 缓解 |
|------|------|
| API Key 泄漏 | 仅从 K8s Secret / Vault 注入；不入版本库；定期轮转；mxctl `llmproxy rotate-keys` |
| Prompt Injection | 业务侧拼装 prompt 时严格使用 system+user 分段；用户输入不进 system；JSON 输出校验后入库 |
| 数据出境 | air_gapped 强约束；脱敏默认开；审计日志含出网快照（hash） |
| 滥用（恶意用户刷成本） | 租户 + 用户级双限；单次 token cap；月度 USD 硬上限 |
| LLM 误导处置 | observe 模式下 LLM 输出永不直接触发动作；protect 模式下动作仍由规则/ML 主导，LLM 仅解释 |
| 模型供应商封停 | Fallback 链 + 厂商池冗余（至少 2 家公网 + 1 家本地） |
| 审计篡改 | 审计入 Kafka + ClickHouse + WORM 选项（KA） |

---

## 21. 关键代码路径

```
cmd/server/llmproxy/main.go                       # 入口
internal/server/llmproxy/server/                  # gRPC server + middleware
internal/server/llmproxy/router/                  # 场景路由 + 黑名单 + 在线探测
internal/server/llmproxy/provider/                # driver 集合
   ├── types.go
   ├── factory.go
   ├── openai/
   ├── anthropic/
   ├── google/
   └── openai_compat/
internal/server/llmproxy/cache/                   # Redis 缓存
internal/server/llmproxy/quota/                   # 配额 + 计费
internal/server/llmproxy/sanitize/                # 脱敏
internal/server/llmproxy/fallback/                # 兜底文案
internal/server/llmproxy/audit/                   # Kafka 审计 producer
internal/server/llmproxy/config/                  # YAML + 租户覆盖加载
api/proto/llmproxy/v1/llmproxy.proto              # gRPC 接口定义

# 业务侧 client 封装
internal/server/manager/biz/llm_assist_v2.go      # 替代旧 llm_assist.go
internal/server/engine/enricher/llm.go            # Engine 异步增强
```

---

## 22. 与对标产品对照

| 维度 | mxsec LLMProxy | 青藤万象 LLM 模块 | 蜂巢 LLM 模块 |
|------|----------------|------------------|---------------|
| 多厂商 | 9 家公网 + 3 类本地 | 闭源，绑定厂商 | 同闭源 |
| 离网/信创 | 默认支持 + 强约束 | 需定制 | 需定制 |
| 多租户配额 | from-day-1 | 中后期补 | 中后期补 |
| 场景路由 | 4 大场景 + 可扩展 | 单告警解释 | 单告警解释 |
| 兜底文案 | 内置规则模板 | 直接报错 | 直接报错 |
| 缓存命中率 | 50%+ 目标 | 未披露 | 未披露 |
| 审计完整度 | 每次调用 90d 留存 + token/cost 全栈 | 仅成功调用 | 仅成功调用 |
| 开源 | 是 | 闭源 | 闭源 |

---

## 23. 参考文档

- [`architecture.md`](architecture.md) §2.6 — LLMProxy 在六微服务中的定位
- [`operating-modes.md`](operating-modes.md) — LLM 输出不绕过 observe/protect 规则
- [`multi-tenant.md`](multi-tenant.md) §7 §8 — 租户级配置覆盖 + 计费
- [`ml-models.md`](ml-models.md) — 本地 ML（与 LLM 互补的"实时检测层"）
- [`engine-design.md`](engine-design.md) — Engine 何时异步调用 LLMProxy
- [`api-reference.md`](api-reference.md) — `/api/v2/llm/*` 业务侧 REST 接口
- `ref/00-总体评估与商业化路线.md` — LLM 商业化档位（Baseline / Smart / AI-Native）
- `ref/01-服务端架构.md` — 旧 Manager 内嵌 `llm_assist` 现状
- `ref/appendix/蜂巢-能力清单.md` §LLM 差异化 — mxsec 在"研判"环节的差异化定位
