# Gateyes - 为Agent设计的网关

**🛡️ 为AI Agent提供智能路由、安全防护和可靠性保障的企业级网关**

---

## 🎯 核心定位

Gateyes 是一个专为 **AI Agent** 设计的网关和铠甲系统，提供：

- 🔀 **智能路由** - 多种路由策略、自动故障转移、负载均衡、自定义规则
- 🛡️ **安全防护** - PII检测、内容过滤、Prompt注入防护
- 🔌 **MCP防护** - Model Context Protocol连接保护和监控（可插拔）
- 📊 **可观测性** - 详细的指标、追踪和健康检查
- ⚡ **高性能** - Go语言实现，低延迟，高并发
- 🔧 **可插拔** - 所有功能模块化，按需启用

---

## 🚀 快速开始

### 本地运行

```bash
# 克隆仓库
git clone https://github.com/yourusername/gateyes.git
cd gateyes

# 构建
go build -o gateyes ./cmd/gateyes

# 运行
./gateyes -config config/gateyes.json
```

### Docker部署

```bash
# 构建镜像
docker build -t gateyes:latest .

# 运行容器
docker run -p 8080:8080 -v $(pwd)/config:/config gateyes:latest
```

---

## 📋 核心功能

### 1️⃣ 智能路由系统

支持多种路由策略，确保请求总是发送到最优的LLM提供商。

#### 路由策略

| 策略 | 描述 | 适用场景 |
|------|------|----------|
| **Round-Robin** | 轮询分配请求 | 负载均衡 |
| **Least-Latency** | 选择延迟最低的提供商 | 性能优先 |
| **Weighted** | 按权重分配流量 | 灰度发布 |
| **Cost-Optimized** | 选择成本最低的提供商 | 成本优化 |
| **Priority** | 按优先级顺序选择 | 主备模式 |

#### 自定义路由规则

支持用户自定义路由规则，基于请求内容、用户属性等动态路由：

```json
{
  "custom_rules": [
    {
      "name": "Route GPT-4 to Azure",
      "priority": 100,
      "conditions": [
        {"type": "body", "operator": "contains", "value": "gpt-4"}
      ],
      "action": {"type": "route", "provider": "azure-openai"}
    }
  ]
}
```

#### 故障转移和重试

- 自动故障转移到备用提供商
- 可配置的重试策略（指数退避）
- 熔断器保护
- 健康检查和自动恢复

---

### 2️⃣ MCP防护机制（可插拔）

为Model Context Protocol连接提供全方位保护，确保Agent与MCP服务器的通信稳定可靠。

#### 核心功能

- 🏥 **健康检查** - 定期检测MCP服务器健康状态
- 🔌 **熔断器** - 防止级联故障
- 🔗 **连接池** - 复用连接，提高性能
- 📊 **异常检测** - 实时监控错误率和延迟
- ⏱️ **超时控制** - 防止请求hang住
- 🔔 **告警通知** - Webhook告警集成

#### 配置示例

```json
{
  "mcp_guard": {
    "enabled": true,
    "health_check": {
      "enabled": true,
      "interval": "30s",
      "unhealthy_threshold": 3
    },
    "circuit_breaker": {
      "enabled": true,
      "failure_threshold": 5
    },
    "anomaly_detection": {
      "enabled": true,
      "error_rate_threshold": 0.5
    }
  }
}
```

---

### 3️⃣ Agent保护层（Guardrails）

多层安全防护，保护Agent免受恶意输入和输出的影响。

#### 防护功能

| 功能 | 描述 | 动作 |
|------|------|------|
| **PII检测** | 检测和脱敏敏感信息（邮箱、电话、SSN等） | 脱敏/拒绝 |
| **内容过滤** | 过滤有害内容和Prompt注入 | 拒绝 |
| **响应验证** | 验证响应格式和大小 | 拒绝 |
| **异常检测** | 检测异常行为模式 | 警告/拒绝 |
| **自定义规则** | 用户自定义安全规则 | 可配置 |

#### PII检测示例

输入: "My email is john@example.com and phone is 555-1234"
输出: "My email is [REDACTED] and phone is [REDACTED]"

#### Prompt注入防护

自动检测和阻止常见的Prompt注入攻击模式。

---

### 4️⃣ 响应缓存系统

智能缓存LLM响应，显著降低API成本和延迟。

#### 缓存后端

| 后端 | 描述 | 适用场景 |
|------|------|----------|
| **Memory** | 内存LRU缓存 | 单实例部署 |
| **Redis** | Redis分布式缓存 | 多实例部署 |

#### 核心功能

- 🔑 **智能缓存键** - 基于provider、model和messages生成唯一键
- ⏱️ **可配置TTL** - 灵活的缓存过期时间
- 📊 **缓存统计** - 命中率、大小等详细统计
- 🔄 **LRU淘汰** - 内存缓存使用LRU策略
- 🌐 **分布式支持** - Redis后端支持多实例共享缓存

#### 配置示例

```json
{
  "cache": {
    "enabled": true,
    "backend": "memory",
    "ttl": "1h",
    "max_size": 104857600,
    "max_entries": 10000
  }
}
```

#### 使用Redis

```json
{
  "cache": {
    "enabled": true,
    "backend": "redis",
    "ttl": "2h",
    "redis_addr": "localhost:6379",
    "redis_password": "",
    "redis_db": 0
  }
}
```

#### 缓存效果

- **成本节省**: 缓存命中可节省30-70%的API成本
- **延迟降低**: 缓存响应延迟 < 5ms
- **自动管理**: LRU自动淘汰旧条目

---

### 5️⃣ 多提供商支持

统一的OpenAI兼容接口，支持多个LLM提供商：

- OpenAI
- Anthropic (Claude)
- Azure OpenAI
- Google Gemini (计划中)
- AWS Bedrock (计划中)
- 本地模型 (Ollama) (计划中)

---

### 6️⃣ 可观测性

#### 监控端点

| 端点 | 描述 |
|------|------|
| `/healthz` | 健康检查 |
| `/metrics` | Prometheus指标 |
| `/stats` | 路由统计 |
| `/mcp-stats` | MCP防护统计 |
| `/cache-stats` | 缓存统计 |

---

## 🔧 配置指南

查看 `config/gateyes.example.json` 获取完整配置示例。

### 核心配置项

#### 1. 智能路由配置

```json
{
  "routing": {
    "enabled": true,
    "strategy": "least-latency",
    "fallback": ["anthropic", "azure-openai"],
    "health_check": {"enabled": true, "interval": "30s"},
    "retry": {"enabled": true, "max_retries": 3},
    "circuit_breaker": {"enabled": true, "failure_threshold": 5}
  }
}
```

#### 2. MCP防护配置

```json
{
  "mcp_guard": {
    "enabled": true,
    "health_check": {"enabled": true, "interval": "30s"},
    "circuit_breaker": {"enabled": true},
    "connection_pool": {"enabled": true, "max_connections": 10},
    "anomaly_detection": {"enabled": true, "error_rate_threshold": 0.5}
  }
}
```

#### 3. Guardrails配置

```json
{
  "policy": {
    "enabled": true,
    "guardrails": {
      "enabled": true,
      "pii_detection": {"enabled": true, "redact": true},
      "content_filter": {"enabled": true, "block_prompt_injection": true},
      "anomaly_detection": {"enabled": true}
    }
  }
}
```

---

## 📖 使用示例

### 基本请求

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### 指定提供商

```bash
# 通过Header指定
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "X-Gateyes-Provider: anthropic" \
  -d '...'

# 通过Query参数指定
curl -X POST "http://localhost:8080/v1/chat/completions?provider=anthropic" \
  -d '...'
```

### Agent到MCP请求

```bash
curl -X POST http://localhost:8080/mcp/tools/list \
  -H "Authorization: Bearer your-api-key"
```

### 查看统计信息

```bash
# 路由统计
curl http://localhost:8080/stats

# MCP防护统计
curl http://localhost:8080/mcp-stats

# 缓存统计
curl http://localhost:8080/cache-stats

# Prometheus指标
curl http://localhost:8080/metrics
```

---

## 🏗️ 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                         Gateyes                              │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │              Middleware Chain                       │    │
│  │  Logging → Metrics → RateLimit → Auth → Guardrails │    │
│  └────────────────────────────────────────────────────┘    │
│                           ↓                                  │
│  ┌────────────────────────────────────────────────────┐    │
│  │              Smart Router                           │    │
│  │  • Custom Rules                                     │    │
│  │  • Strategy Selection (RR/LL/Weighted/Cost)        │    │
│  │  • Health Check                                     │    │
│  │  • Circuit Breaker                                  │    │
│  │  • Retry Logic                                      │    │
│  └────────────────────────────────────────────────────┘    │
│                           ↓                                  │
│  ┌─────────────┬──────────────┬──────────────┬─────────┐  │
│  │   OpenAI    │  Anthropic   │ Azure OpenAI │   MCP   │  │
│  │   Proxy     │    Proxy     │    Proxy     │  Guard  │  │
│  └─────────────┴──────────────┴──────────────┴─────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## 🎨 设计理念

### 1. Agent-First设计

专为AI Agent设计，理解Agent的特殊需求：

- **可靠性优先** - 自动故障转移，确保Agent不会因单点故障停止工作
- **安全防护** - 多层防护，保护Agent免受恶意输入
- **MCP支持** - 原生支持Model Context Protocol
- **可观测性** - 详细的监控和追踪

### 2. 可插拔架构

所有功能都是可插拔的，通过配置文件启用/禁用：

```json
{
  "routing": {"enabled": true},
  "mcp_guard": {"enabled": true},
  "policy": {"guardrails": {"enabled": true}}
}
```

### 3. 性能优先

- **Go语言实现** - 高性能、低延迟
- **连接池** - 复用连接，减少开销
- **并发处理** - 充分利用多核CPU
- **轻量级** - 单二进制部署

---

## 🛣️ 路线图

### Phase 1: 核心功能 ✅
- [x] 智能路由系统
- [x] MCP防护机制
- [x] Agent保护层（Guardrails）
- [x] 自定义路由规则
- [x] 响应缓存系统

### Phase 2: 扩展功能 (计划中)
- [ ] 更多LLM提供商支持
- [ ] 增强可观测性（OpenTelemetry）
- [ ] 成本追踪和预算管理
- [ ] 语义缓存

### Phase 3: 高级功能 (计划中)
- [ ] Prompt管理和版本控制
- [ ] A/B测试和金丝雀部署
- [ ] Web UI管理界面
- [ ] 多租户支持

---

## 📄 许可证

MIT License

---

## 🙏 致谢

感谢所有贡献者和开源社区的支持！
