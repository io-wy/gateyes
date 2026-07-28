# Gateyes 缓存与请求改写：面试叙事稿

> 这份文档不是实现说明书，而是面试表达稿。
> 重点是：问题怎么定义、方案怎么选、数据怎么证明、消融怎么做。

---

## 1. 一句话总结

我做的不是“单纯加一个缓存”，而是把缓存前面的请求稳定性问题一起解决了。

核心思路是：先把请求改写成稳定、可归一的形态，再进入缓存、路由和观测链路。这样才能把命中率、可解释性和跨 provider 的一致性一起做出来。

---

## 2. 这个问题为什么值得做

LLM 网关里的缓存，最容易失败的地方不是存储，而是 key 不稳定。

常见噪声包括：

- 上游 SDK 注入的随机字段
- prompt 里顺序不稳定的 JSON
- 请求体里的临时标记、trace 字段、bucket 标记
- 同一个语义请求，在不同入口上形成不同 key

如果直接做精确匹配缓存，会出现两个问题：

1. 命中率低，缓存看起来“没用”
2. 同一类请求被切成太多 key，观测也会失真

所以正确的切入点不是“再堆一个 cache backend”，而是“先做请求归一化，再做缓存策略”。

---

## 3. 我会怎么讲这个实现

### 3.1 请求改写层

参考项目最有价值的地方，是把改写当作一等公民：

- 清理无业务意义的随机字段
- 统一 prompt 结构
- 重写 cache key 相关字段
- 让请求在进入路由和缓存前先稳定下来

在 Gateyes 里，这个思路可以落成三层：

1. `rewrite`：去噪、归一化、补默认值
2. `cache key`：基于 canonical payload 生成稳定 key
3. `affinity`：把高复用请求固定到同一 provider / 同一 session

### 3.2 缓存层

Gateyes 已经有 L1 精确匹配缓存，适合承接这类改写后的稳定请求。

可以这样讲：

- Redis 负责跨副本共享
- Memory LRU 负责故障降级和热点兜底
- 流式和非流式分开处理
- 命中后直接回放完整 response，而不是重跑上游

### 3.3 可解释性层

这是面试里很好讲的一点。

不是简单说“有缓存”，而是能解释：

- 为什么这次命中了
- 为什么那次没命中
- 是改写前后 key 变了，还是 provider 侧变了
- 是 bucket 不同，还是 stream 状态不同

这会把缓存从“黑盒”变成“可调参系统”。

---

## 4. 可以直接拿来讲的数据口径

下面这些是面试里最有说服力的指标，不一定都要报绝对值，但口径要统一。

### 4.1 缓存侧

- cache hit rate
- cache miss rate
- upstream offload ratio
- p95 / p99 latency
- cache write amplification
- key cardinality

### 4.2 路由侧

- 同 session 落同 provider 的比例
- prefix-affinity 命中率
- provider 侧 prompt cache / KV cache 命中情况

### 4.3 稳定性侧

- Redis 故障时的降级成功率
- Memory cache 兜底命中率
- 改写前后 key 数量变化
- 同语义请求被拆成多少个 key

### 4.4 我们现有体系里能说的硬数据

可以直接引用这些实现级数据来增强可信度：

- `BuildKey` 约 `450 ns/op`
- `CanonicalizeJSON` 约 `6.6 μs/op`
- `MemoryCache.Get` 约 `60 ns/op`
- `RedisCache.Get` 约 `45 μs/op`
- `internal/service/cache` 测试覆盖约 `90.9%`

这组数据适合用来说明：缓存不是瓶颈，真正贵的是 key 规范化，所以改写和归一化是值得做的。

---

## 5. 消融实验怎么设计

面试时最有分量的，不是“我做了缓存”，而是“我知道哪个组件带来多少收益”。

### 5.1 实验基线

固定同一批请求，至少覆盖三类场景：

- 高重复问答
- 相同 system prompt + 不同 user prompt
- 流式请求

### 5.2 消融分组

#### A. Baseline

- 只做原始请求透传
- 不做 rewrite
- 不做 cache
- 不做 affinity

#### B. + Cache

- 加精确匹配缓存
- 观察命中率和延迟下降

#### C. + Rewrite

- 在缓存前做请求归一化
- 对比 key 数量、命中率、上游请求数

#### D. + Bucket / Hint

- 加 `bucket` 维度
- 验证不同业务线隔离后，是否还能保住高频请求的命中

#### E. + Affinity

- 加 session / prefix affinity
- 观察 provider 侧 prefix cache 是否提升

### 5.3 每组看什么

每组至少报这五个数：

- 请求数
- cache hit rate
- p95 latency
- upstream call reduction
- provider 侧 prefix cache / cached token ratio

### 5.4 你在面试里可以怎么说

> “我不是只看缓存命中率，而是做了分组消融。单加缓存只能说明存储有效；加上请求改写后，key cardinality 才真正收敛；再加 affinity，provider 侧的 prefix cache 才被放大。最后看的是整条链路的综合收益，而不是单点指标。”

---

## 6. 推荐的讲述顺序

1. 先讲问题：LLM 网关的瓶颈不是算力，而是请求不稳定导致缓存失效
2. 再讲方案：先 rewrite，再 cache，再 affinity
3. 再讲证据：key 收敛、命中率、p95、上游调用下降
4. 最后讲边界：不是做语义缓存，而是先把 exact-match 做到极致

---

## 7. 可直接背的版本

我在 Gateyes 里处理缓存时，没有把它当成一个独立模块，而是当成“请求稳定性 + 缓存 + 路由”的组合问题。因为 LLM 请求里的噪声很多，直接做精确缓存命中率很差。我的做法是先在请求进入网关时做归一化和改写，把随机字段、无业务意义的标记以及不稳定的 prompt 结构清掉，再用 canonical payload 生成稳定 key。这样缓存、路由和观测看到的是同一个语义请求，不会被 SDK 或上游注入的噪声拆散。

从结果看，真正提升不是单点缓存命中，而是三件事一起提升：key 数量收敛、cache hit rate 上升、provider 侧 prefix cache 更容易被放大。我们还专门做了消融，把 rewrite、bucket、affinity 分开验证，避免把收益都算到缓存头上。

---

## 8. 面试时别乱说的点

- 不要把 exact-match 说成语义缓存
- 不要把“缓存命中”说成“模型更聪明”
- 不要只报一个命中率，不报延迟和上游节省
- 不要把 provider 侧 prefix cache 和 gateway 侧 response cache 混成一个东西
