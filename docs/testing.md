# 测试策略与质量保障

> 适用版本：v0.2.0+
> 最后更新：2026-05-03

---

## 测试哲学

本项目遵循 **TDD（测试驱动开发）** 工作流：

1. 写测试（RED）
2. 运行测试，确认失败
3. 写最小实现（GREEN）
4. 运行测试，确认通过
5. 重构（IMPROVE）
6. 验证覆盖率 ≥ 80%

所有公共函数必须有对应测试。HTTP handler 变更用 `httptest.NewRecorder` 写集成测试。数据库变更用 SQLite in-memory 写测试。外部服务调用用 mock，不依赖真实上游。

---

## 测试类型

### 1. 单元测试（Unit Tests）

- 目标：单个函数、工具类、独立组件
- 位置：与被测代码同包，`_test.go` 后缀
- 工具：Go 标准库 `testing`，`testify/assert`（可选）
- 要求：快速（< 1s）、无外部依赖、可并行运行

**典型覆盖**：
- `internal/service/cache` — MemoryCache LRU/TTL/并发安全
- `internal/service/router` — affinity 哈希计算、策略排序
- `internal/service/limiter` — TokenBucket 算法正确性
- `internal/config` — 配置验证、热重载、环境变量解析

### 2. 集成测试（Integration Tests）

- 目标：API endpoint、数据库操作、多组件协作
- 位置：同包 `_test.go`，或 `internal/handler/*_test.go`
- 工具：`httptest.NewServer`，`sqlx` + SQLite `:memory:`，`miniredis`
- 要求：验证组件间交互正确，允许秒级耗时

**典型覆盖**：
- `internal/handler` — HTTP 路由、鉴权中间件、admin CRUD
- `internal/repository/sqlstore` — 数据库 CRUD、事务、迁移
- `internal/service/responses` — 完整请求链路（prepare→stream→persist）
- `internal/middleware` — Guard、Auth、限流、Trace

### 3. E2E 测试（End-to-End）

- 目标：关键用户流程的端到端验证
- 位置：`benchmark/` 目录下的压测工具兼做 E2E
- 工具：自定义压测客户端、Mock Upstream
- 要求：验证完整系统行为，包括流式响应、熔断、降级

**覆盖场景**：
- Responses API 非流式 + 流式完整流程
- Chat Completions 兼容性转换
- Anthropic Messages 兼容性转换
- Embeddings 向量化
- 高并发下的限流和熔断行为

---

## 覆盖率报告

运行 `go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out`。

### 核心包覆盖率（2026-05-03）

| 包 | 覆盖率 | 状态 |
|----|--------|------|
| `internal/service/budget` | **100.0%** | ✅ |
| `internal/trace` | **95.0%** | ✅ |
| `internal/repository` | **94.1%** | ✅ |
| `internal/service/cache` | **90.9%** | ✅ |
| `internal/redis` | **90.9%** | ✅ |
| `internal/service/router` | **84.4%** | ✅ |
| `internal/config` | **83.7%** | ✅ |
| `internal/service/auth` | **83.6%** | ✅ |
| `internal/service/alert` | **81.1%** | ✅ |
| `internal/service/provider` | **80.7%** | ✅ |
| `internal/service/responses` | **78.9%** | ⚠️ |
| `internal/service/limiter` | **77.9%** | ⚠️ |
| `internal/repository/sqlstore` | **77.5%** | ⚠️ |
| `internal/middleware` | **75.0%** | ⚠️ |
| `internal/db` | **75.6%** | ⚠️ |
| `internal/handler` | **72.0%** | ⚠️ |
| `internal/service/catalog` | **65.5%** | ⚠️ |
| `utils/error` | **74.1%** | ⚠️ |

**总计**：**73.0%**（含 benchmark/cmd 等 0% 包拉低均值；核心业务包均值 > 80%）

### 待提升项

| 包 | 缺口 | 原因 |
|----|------|------|
| `internal/handler` | 72.0% | 部分 admin handler 边界分支未覆盖 |
| `internal/service/catalog` | 65.5% | Service Catalog 部分功能在持续迭代中 |
| `internal/service/responses` | 78.9% | stream 复杂分支、错误恢复路径 |
| `internal/service/limiter` | 77.9% | Redis 故障分支、队列满边界 |

---

## Benchmark 体系

### 运行方式

```bash
# 全部 benchmark
go test -bench=. -benchmem ./...

# 指定包
go test -bench=. -benchmem ./internal/service/cache
go test -bench=. -benchmem ./internal/service/router
go test -bench=. -benchmem ./internal/service/limiter
```

### 核心性能基准

#### Cache 层

| Benchmark | 操作 | 耗时 | allocs |
|-----------|------|------|--------|
| MemoryCache_Get | 内存缓存查找 | ~60 ns/op | 1 |
| MemoryCache_Set | 内存缓存写入 | ~25 ns/op | 0 |
| BuildKey | 缓存 key 生成 | ~450 ns/op | 13 |
| CanonicalizeJSON | JSON 规范化 | ~6.6 μs/op | 97 |
| RedisCache_Get | Redis 查找 | ~45 μs/op | 26 |

#### Router 层

| Benchmark | 操作 | 耗时 | allocs |
|-----------|------|------|--------|
| Router_Select | 完整选择（含过滤+策略） | ~270 ns/op | 4 |
| Router_OrderCandidates | 完整排序（含规则+排序+亲和） | ~1.05 μs/op | 11 |
| SessionAffinity_Pin | Session 哈希固定 | ~90 ns/op | 1 |
| PrefixAffinity_Pin | Prefix 哈希固定 | ~500 ns/op | 4 |
| CompositeAffinity_Pin | 组合亲和 | ~110 ns/op | 1 |

#### Limiter 层

| Benchmark | 操作 | 耗时 | allocs |
|-----------|------|------|--------|
| TokenBucket_TryConsume | 令牌桶扣减 | ~16 ns/op | 0 |
| Limiter_Allow (Redis) | 分布式限流判定 | ~50 μs/op | 20+ |
| Limiter_Allow (Memory) | 内存限流判定 | ~5 μs/op | 5+ |

---

## 测试工具链

| 工具 | 用途 | 配置位置 |
|------|------|----------|
| `go test` | 单元/集成测试 | 标准 |
| `miniredis` | 内存 Redis（限流/cache 测试） | `go.mod` |
| `httptest` | HTTP handler 集成测试 | 标准库 |
| `sqlite` `:memory:` | 数据库测试 | 标准库 |
| `benchmark/` | 压测与 E2E | 项目根目录 |
| `go test -race` | 数据竞争检测 | CI |

---

## CI 中的测试

`.github/workflows/ci.yml` 执行以下检查：

1. `gofmt` — 格式化检查
2. `go test ./...` — 全量单元/集成测试
3. `go test -race ./...` — 数据竞争检测
4. `go vet ./...` — 静态分析
5. `golangci-lint` — 综合 lint
6. `staticcheck` — 深度静态检查
7. `govulncheck` — 漏洞扫描
8. migration smoke test — 数据库迁移验证
9. OpenAPI smoke validation — API 规范校验
10. `gitleaks` — 密钥泄漏检测
11. `trivy` — 文件系统安全扫描

---

## 调试测试失败

1. 先运行单测隔离问题：`go test -v -run TestName ./pkg`
2. 检查测试隔离：是否存在全局状态污染
3. 验证 mock：mock 行为是否与实际代码一致
4. 修复实现，不要修复测试（除非测试本身错误）
5. 使用 `tdd-guide` agent 辅助复杂场景

---

## 测试命名规范

```go
// 单元测试
func TestFunctionName_Scenario(t *testing.T)
func TestFunctionName_Scenario_Expected(t *testing.T)

// 集成测试
func TestEndpoint_Action_Status(t *testing.T)

// Benchmark
func BenchmarkComponent_Operation(b *testing.B)
func BenchmarkComponent_Operation_Parallel(b *testing.B)
```

---

## 新增功能测试清单

在提交前确认：

- [ ] 新增公共函数有对应单元测试
- [ ] HTTP handler 变更有 `httptest` 集成测试
- [ ] 数据库操作变更有 SQLite in-memory 测试
- [ ] 并发代码有 `-race` 检测通过
- [ ] Benchmark 覆盖热路径（如有性能敏感变更）
- [ ] 覆盖率不低于所在包当前水平
