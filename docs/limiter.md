# Limiter 限流机制

多维度令牌桶限流，支持内存和 Redis 两种后端。

## 维度

| 层级 | 维度 | 消费单位 | 后端 |
|------|------|----------|------|
| 全局层 | TPM / RPM | tokens / 1 | Redis（有 Redis 时）或内存 |
| 用户层 | QPS | 1 | 始终内存 |
| 租户层 | TPM / RPM | tokens / 1 | Redis 或内存 |
| Provider 层 | TPM / RPM | tokens / 1 | Redis 或内存 |
| Model 层 | TPM / RPM | tokens / 1 | Redis 或内存 |

Redis 不可用时全部维度自动降级到内存令牌桶（fail-open）。

## 架构

```
Allow(ctx, key, userQPS, tokens)
    |
    v
Request 入队 → consumeLoop 串行消费
    |
    +-- check(): 全局 TPM/RPM + 用户 QPS
    |
    v (通过后)
handler 层: CheckTenant() → CheckProvider() → CheckModel()
    |
    v
任一维度拒绝 → 429
```

`consumeLoop` 内串行执行全局层 + 用户层检查。租户/Provider/Model 在 handler 层按需调用，每个维度独立。

## 配置

```yaml
limiter:
  globalQPS: 1000            # 全局默认用户 QPS
  globalTPM: 1000000         # 全局每分钟 token 上限
  globalTokenBurst: 100000   # 全局 token 桶突发容量
  perUserRequestBurst: 100   # 每用户请求突发容量
  tenantTPM: 0               # 租户 TPM，0=禁用
  providerTPM: 0             # provider TPM，0=禁用
  modelTPM: 0                # model TPM，0=禁用
  queueSize: 1000            # 队列缓冲长度
```

- `*TPM` / `*RPM` 值为 0 时该维度不启用
- burst 为 0 时自动推算为 `TPM/60`，保证最小值为 1

## 算法

**内存令牌桶**：

```
tokens = min(burst, tokens + elapsed_seconds * rate)
if tokens >= n:
    tokens -= n; allow
else:
    deny
```

**Redis 分布式令牌桶**：Lua 原子脚本，Hash 存储 `t`(tokens) 和 `l`(last_fill)，120s TTL。

```
gateyes:rl:g:t              # 全局 TPM
gateyes:rl:ten:{id}:t       # 租户 TPM
gateyes:rl:prov:{name}:t    # provider TPM
gateyes:rl:mod:{name}:t     # model TPM
```

## Token 估算

使用 `prompt_tokens + max_output_tokens` 作为限流消费单位：

```go
func (r *ResponseRequest) EstimateAdmissionTokens() int {
    return r.EstimatePromptTokens() + max(r.MaxOutputTokens, r.MaxTokens, 4096)
}
```

只算 prompt 会导致长输出请求"白嫖"限流。

## 关键行为

1. **userQPS 优先级**：用户配置 QPS > 0 时用用户值，否则 fallback 到 `globalQPS`
2. **context 取消**：`consumeLoop` 处理前先检查 `req.Context.Done()`，已取消的请求返回 false 不消耗 token
3. **优雅停止**：`Stop()` 关闭 `stopCh`，drain 剩余队列，`wg.Wait()` 等待 goroutine 退出
4. **用户桶清理**：超过 10 分钟未访问的用户桶在 `refillLoop` 中自动删除
5. **Redis 切换**：`SetRedis(rdb)` 运行时切换；Redis 不可用时 fail-open 放行
6. **空 ID 跳过**：`CheckTenant/Provider/Model` 在 ID 为空时直接返回 true

## 边界

1. bucketMap（租户/Provider/Model 内存桶）目前不会自动清理，长期运行可能内存膨胀
2. `TPM/60` / `RPM/60` 低值时可能有整数精度损失，已兜底最小值为 1
3. Reload 热更新重建全局桶，但不清理已有用户桶和 bucketMap
4. Redis Cluster 模式下 Lua 脚本需确保 key 路由到同一 slot
