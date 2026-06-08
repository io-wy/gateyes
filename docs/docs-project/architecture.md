# 架构总览

> **定位**：融合本地 GPU 模型与云上 LLM API 的智能网关
>
> 核心能力：智能路由、负载均衡、性能成本优化

---

## 1. 系统定位

Gateyes 是应用与 LLM 推理资源之间的**统一接入层**，同时管理两类上游：

- **本地 GPU 模型**：通过 vLLM、SGLang 等推理引擎部署在私有基础设施或 K8s 集群中
- **云上 LLM API**：OpenAI、Anthropic 等公有云 API 服务

网关负责将两类资源抽象为统一的 OpenAI-compatible 接口，并在此基础上实现智能路由、负载均衡和成本优化。

---

## 2. 架构分层

```
┌─────────────────────────────────────────────────────────────┐
│  Client (SDK / App / 浏览器)                                  │
│  - OpenAI SDK                                               │
│  - Anthropic SDK                                            │
│  - 自定义 HTTP 客户端                                       │
└───────────────────────────┬─────────────────────────────────┘
                            │ HTTPS / SSE
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 1: Ingress / Load Balancer                           │
│  - TLS termination                                          │
│  - Connection management                                    │
└───────────────────────────┬─────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 2: Gateyes Gateway                                   │
│  ├─ 多租户 Auth / RBAC                                      │
│  ├─ 全局限流 / 熔断                                         │
│  ├─ 智能路由（规则引擎 + 策略 + 亲和）                      │
│  ├─ L1 响应缓存（Redis + 内存）                             │
│  ├─ 预算治理 + 用量计费                                     │
│  ├─ 统一 API（Responses / Chat / Messages / Embeddings）    │
│  └─ 可观测性（Metrics / Trace / Audit）                     │
│                                                             │
│  无状态，CPU-only，水平扩展                                 │
└──────────────┬──────────────────────────────┬───────────────┘
               │                              │
               │ HTTP OpenAI API              │ HTTPS
               ▼                              ▼
┌──────────────────────────┐  ┌──────────────────────────────┐
│  本地 GPU 推理集群        │  │  云上 LLM API               │
│  ├─ vLLM Workers         │  │  ├─ OpenAI                 │
│  ├─ SGLang Workers       │  │  ├─ Anthropic              │
│  └─ KServe / Router      │  │  └─ 其他 OpenAI-compatible │
│                          │  │                            │
│  GPU-intensive, 有状态   │  │  按 token / 请求计费       │
└──────────────────────────┘  └──────────────────────────────┘
```

---

## 3. 请求主链路

```
HTTP Request
    │
    ▼
┌─────────────────┐
│  Auth Middleware │  Bearer <key>:<secret> → AuthIdentity
│  (鉴权 + 限流)   │  模型白名单 + Quota 预检查 + 限流
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Handler        │  JSON 绑定 → 协议转换（OpenAI/Anthropic → 内部协议）
│  (协议兼容层)    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  responses.Service
│  (业务编排层)    │
│  ├─ 查询 tenant 可用 provider
│  ├─ registry 过滤（健康/能力/drain）
│  ├─ 构建 RouteContext
│  ├─ 路由选择（规则 → 排序 → 亲和 → 策略）
│  ├─ L1 Cache 查找
│  ├─ 调用上游 provider
│  ├─ 写入 responses + usage
│  └─ 预算扣减
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Provider Adapter
│  (上游协议适配)  │  OpenAI / Anthropic / 本地推理引擎
└────────┬────────┘
         │
         ▼
    Upstream
```

---

## 4. 本地 GPU vs 云上 API 的统一接入

Gateyes 对两类上游使用**同一种接入方式**：`type: openai` + HTTP。

### 4.1 本地 GPU 模型接入

```yaml
providers:
  - name: vllm-llama3
    type: openai
    baseURL: http://vllm-router.llm-ns.svc.cluster.local/v1
    model: meta-llama/Llama-3.1-8B-Instruct
    timeout: 60
    maxTokens: 8192

  - name: sglang-qwen
    type: openai
    baseURL: http://sglang-gateway.llm-ns.svc.cluster.local/v1
    model: Qwen/Qwen2.5-72B-Instruct
    timeout: 60
```

本地推理引擎（vLLM、SGLang）默认暴露 OpenAI-compatible HTTP API，Gateyes 通过标准 HTTP 接入，无需特殊协议适配。

### 4.2 云上 API 接入

```yaml
providers:
  - name: openai-gpt4o
    type: openai
    baseURL: https://api.openai.com/v1
    apiKey: ${OPENAI_API_KEY}
    model: gpt-4o

  - name: anthropic-claude
    type: anthropic
    baseURL: https://api.anthropic.com
    apiKey: ${ANTHROPIC_API_KEY}
    model: claude-3-5-sonnet-20241022
```

### 4.3 混合部署拓扑

```
                    ┌─────────────────┐
                    │   Gateyes       │
                    │   (K8s Deployment, CPU)
                    └────────┬────────┘
                             │
           ┌─────────────────┼─────────────────┐
           │                 │                 │
           ▼                 ▼                 ▼
    ┌────────────┐   ┌────────────┐   ┌────────────────┐
    │ vLLM Router│   │SGLang Model│   │ OpenAI API     │
    │ (HTTP)     │   │ Gateway    │   │ (HTTPS)        │
    └─────┬──────┘   │ (HTTP)     │   └────────────────┘
          │          └─────┬──────┘
    ┌─────┴─────┐    ┌─────┴─────┐
    ▼           ▼    ▼           ▼
┌───────┐  ┌───────┐┌───────┐  ┌───────┐
│vLLM   │  │vLLM   ││SGLang │  │SGLang │
│Pod 1  │  │Pod 2  ││Pod 1  │  │Pod 2  │
│(GPU)  │  │(GPU)  ││(GPU)  │  │(GPU)  │
└───────┘  └───────┘└───────┘  └───────┘
```

---

## 5. 核心设计决策

### 5.1 Provider-Native Adapter（不做协议抹平）

每种 provider 保留其原生能力：
- **OpenAI adapter**：支持 tool_call、function_call、json_schema
- **Anthropic adapter**：支持 thinking、tool_use、citations
- **本地推理引擎**：通过标准 OpenAI API 接入，完整保留模型能力

### 5.2 统一内部协议

外部多路 API（responses / chat / messages）收敛到一套内部协议：
- `ResponseRequest / Response / ResponseEvent / Usage`
- 新增 provider 只需实现 `Provider` 接口

### 5.3 无状态设计

Gateyes 本身无状态，所有状态外置：
- 配置 → ConfigMap
- 密钥 → Secret / 环境变量
- 限流状态 → Redis
- 缓存 → Redis + 内存 LRU
- 持久化数据 → PostgreSQL / SQLite

这意味着可以任意水平扩缩容，不受单机限制。

---

## 6. 与下层平台的职责边界

| 功能 | Gateyes (Layer 2) | 下层平台 (Layer 3-4) |
|------|-------------------|----------------------|
| 多租户 Auth | ✅ | ❌ |
| 跨提供商路由 | ✅（OpenAI + Anthropic + 自托管） | ❌ |
| 预算/计费 | ✅ | ❌ |
| 统一 API 入口 | ✅ | ❌ |
| 模型部署 | ❌ | ✅（KServe / Helm / 裸机） |
| GPU 调度 | ❌ | ✅（K8s + GPU Operator） |
| KV Cache 管理 | ❌ | ✅（vLLM / SGLang 内部） |
| Prefix-aware 路由 | 基础版（affinity 层） | 高级版（Production Stack Router） |
| 自动扩缩容 | ❌ | ✅（KEDA / HPA） |

**原则**：Gateyes 做好"统一入口 + 多租户治理 + 跨提供商编排"，下层平台做好"模型部署 + GPU 调度 + 智能路由"。两者通过 HTTP OpenAI API 对接。
