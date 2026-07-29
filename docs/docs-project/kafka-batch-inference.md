# Kafka EventBus and Batch Inference

> 最后更新：2026-07-29

Gateyes 的异步路径现在以 Kafka 作为 durable eventbus。在线 `/v1/*` 推理请求仍然保持低延迟同步返回；审计、响应详情、响应状态更新、batch inference item 等可序列化任务通过 Kafka topic 投递和消费。

---

## 1. EventBus 定位

`internal/pkg/eventbus` 保留两类入口：

| API | 用途 |
| --- | --- |
| `Publish(func(ctx context.Context))` | 进程内 best-effort 异步任务，适合 alert/callback 等非持久工作 |
| `PublishEvent(ctx, Event)` | Kafka durable typed event，适合响应详情、状态更新、batch item |

Kafka event 格式：

```json
{
  "key": "batch-or-response-id",
  "type": "batch:item",
  "payload": "base64/json bytes"
}
```

当前事件类型：

| Type | Producer | Consumer |
| --- | --- | --- |
| `response:details` | `sqlstore.CreateResponse` | `sqlstore.handleResponseDetailsEvent` |
| `response:update` | `responses.persistSuccess` | `responses.handleUpdateResponseEvent` |
| `batch:item` | `batch.Service.Create` | `batch.Service.handleBatchItemEvent` |

Kafka 写入失败时会降级为进程内 dispatch，避免在线请求直接丢数据；生产环境应通过 broker、topic、consumer lag、eventbus dropped metrics 监控持久化质量。

---

## 2. 配置

```yaml
persistence:
  busBuffer: 10000
  busWorkers: 8
  handlerTimeoutSeconds: 5
  kafka:
    enabled: true
    brokers:
      - kafka-0.kafka:9092
      - kafka-1.kafka:9092
    topic: gateyes.events
    consumerGroup: gateyes
    clientID: gateyes-gateway
    batchSize: 100
    batchTimeoutMs: 50
    readMinBytes: 1
    readMaxBytes: 10485760
    maxAttempts: 3
```

建议 topic：

- `gateyes.events`：当前统一事件 topic，按 event key 分区。

后续如果 batch 量很大，可以拆成：

- `gateyes.events.persistence`
- `gateyes.events.batch`
- `gateyes.events.audit`

---

## 3. Batch Inference API

创建 batch：

```bash
curl -X POST http://127.0.0.1:8028/v1/batches \
  -H "Authorization: Bearer <api_key>:<api_secret>" \
  -H "Content-Type: application/json" \
  -d '{
    "endpoint": "/v1/responses",
    "model": "gpt-4.1-mini",
    "requests": [
      {"custom_id": "task-1", "body": {"input": "summarize A"}},
      {"custom_id": "task-2", "body": {"input": "summarize B"}}
    ]
  }'
```

查询 batch：

```bash
curl http://127.0.0.1:8028/v1/batches/<batch_id> \
  -H "Authorization: Bearer <api_key>:<api_secret>"
```

查询 item：

```bash
curl http://127.0.0.1:8028/v1/batches/<batch_id>/items \
  -H "Authorization: Bearer <api_key>:<api_secret>"
```

支持 endpoint：

| Endpoint | Worker surface |
| --- | --- |
| `/v1/responses` | `responses` |
| `/v1/chat/completions` | `chat` |
| `/v1/messages` | `messages` |

batch item 会强制 `stream=false`。如果 item body 未设置 `model`，会继承 batch 顶层 `model`。

---

## 4. 数据流

```text
POST /v1/batches
  -> auth middleware
  -> batch_jobs insert
  -> batch_items insert
  -> Kafka PublishEvent(type=batch:item, key=batch_id)
  -> consumer group worker
  -> responses.Service.Create
  -> provider route/cache/retry/fallback
  -> response record + usage/budget persistence
  -> batch_items completed/failed
  -> batch_jobs counter/status refresh
```

DB 是状态真相源：

- `batch_jobs`：batch 元数据、总数、完成数、失败数、状态。
- `batch_items`：单条请求、响应、错误、关联 response id。

Kafka 是调度和解耦层：

- 多 gateway 实例通过同一 consumer group 分摊 batch item。
- handler 成功后 commit offset。
- handler 失败不 commit，Kafka 会 redeliver。
- item 终态更新幂等，重复消费不会造成无限 redelivery。

---

## 5. 面试口径

可以这样讲：

> 我把 Gateyes 从同步 AI API 网关扩展成了 AI 网关推理平台。在线请求仍走低延迟路径；所有可序列化的异步任务统一抽象成 typed event，通过 Kafka 做 durable eventbus。批量推理不是另起一套 provider 调用逻辑，而是把 batch item 投递到 Kafka，由 worker 复用现有 responses.Service，因此路由、缓存、重试、fallback、usage、budget、trace 都是一套机制。DB 负责 batch 状态，Kafka 负责削峰和跨实例调度。
