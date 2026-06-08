---
name: test-strategy
version: 1.0.0
description: 测试策略分层。定义测试金字塔比例、每类测试的编写规范、契约测试/混沌测试/快照测试模板。新模块开发或测试覆盖率不足时触发。Triggered when writing tests, improving coverage, designing test architecture, or when user says "测试策略/test strategy/test pyramid/契约测试/chaos test/覆盖率/coverage/写测试/单元测试/unit test/集成测试/integration test/测试模板".
---

# Test Strategy

> 核心：不是"写测试"，是"设计测试体系"。单元测试保正确，集成测试保协同，E2E 保体验。

## 触发条件

- 新模块开发（先设计测试策略再写测试）
- 测试覆盖率 < 80%
- io-wy 说「测试策略」「test strategy」「test pyramid」「契约测试」「chaos test」
- CLAUDE.md C-09 测试约束触发时的进阶指导

## 测试金字塔

```
        /  E2E  \           10% — 端到端，验证完整用户流程
       / 集成测试 \          20% — 模块间协同，API+DB+外部服务
      /  单元测试  \         70% — 函数级，快速、隔离、无副作用
```

### 比例不是硬性规定，是指导原则

- 写新功能时：先写单元测试，再写集成测试
- 修 bug 时：先写一个失败的测试复现 bug，再修复
- 重构时：确保现有测试全部通过，测试就是安全网

## 一、单元测试（70%）

### 规范

- 每个公共函数至少一个测试
- 测试文件与源文件同目录：`service.go` → `service_test.go`
- 测试函数命名：`Test<函数名>_<场景>_<期望>`
- 表驱动测试优先

### 模板

```go
func TestCalculatePrice(t *testing.T) {
    tests := []struct {
        name      string
        base      float64
        discount  float64
        want      float64
        wantErr   bool
    }{
        {name: "normal", base: 100, discount: 0.2, want: 80, wantErr: false},
        {name: "zero_discount", base: 100, discount: 0, want: 100, wantErr: false},
        {name: "negative_base", base: -1, discount: 0.2, want: 0, wantErr: true},
        {name: "discount_over_one", base: 100, discount: 1.5, want: 0, wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := CalculatePrice(tt.base, tt.discount)
            if (err != nil) != tt.wantErr {
                t.Errorf("CalculatePrice() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr && got != tt.want {
                t.Errorf("CalculatePrice() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### 反例免疫约束

```go
// WRONG: 测试名字不含意图
func TestHandler(t *testing.T) { ... }

// CORRECT: 测试名字描述场景和期望
func TestHandler_CreateUser_DuplicateEmail_ReturnsConflict(t *testing.T) { ... }
```
Why: 3 个月后看测试报告，TestHandler 失败了，完全不知道什么场景挂了。

```go
// WRONG: 测试只走 happy path
func TestGetUser(t *testing.T) {
    got := GetUser("valid-id")
    assert.NotNil(t, got)
}

// CORRECT: 覆盖 happy path + 边界 + 错误
func TestGetUser_ValidID_ReturnsUser(t *testing.T) { ... }
func TestGetUser_EmptyID_ReturnsError(t *testing.T) { ... }
func TestGetUser_NotFound_ReturnsError(t *testing.T) { ... }
```
Why: 只测 happy path 等于没有测试，线上出问题的都是 edge case。

## 二、集成测试（20%）

### 规范

- HTTP handler：用 `httptest.NewRecorder`
- 数据库：用 SQLite in-memory 或 Docker 容器
- 外部服务：用 mock server（`httptest.NewServer`）
- 不依赖外部环境，`go test` 就能跑

### HTTP Handler 模板

```go
func TestAPI_CreateUser_Integration(t *testing.T) {
    // Setup
    db := setupTestDB(t)       // SQLite in-memory
    defer db.Close()
    handler := NewHandler(db)
    router := setupRouter(handler)

    // Request
    body := strings.NewReader(`{"email":"test@example.com","name":"test"}`)
    req := httptest.NewRequest("POST", "/api/v1/users", body)
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()

    // Execute
    router.ServeHTTP(w, req)

    // Assert
    assert.Equal(t, 201, w.Code)
    var resp map[string]interface{}
    json.Unmarshal(w.Body.Bytes(), &resp)
    assert.Equal(t, "test@example.com", resp["email"])
}
```

### 数据库测试模板

```go
func setupTestDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("open test db: %v", err)
    }
    // 执行 migration
    _, err = db.Exec(schemaSQL)
    if err != nil {
        t.Fatalf("migrate test db: %v", err)
    }
    return db
}
```

## 三、E2E 测试（10%）

### 规范

- 只覆盖核心用户流程（注册→登录→操作→退出）
- 测试环境用 docker-compose 拉起完整服务栈
- 不 mock，走真实网络
- 标记 `//go:build e2e`，日常 CI 不跑，release 前跑

### 模板

```go
//go:build e2e

package e2e

import (
    "net/http"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestE2E_UserWorkflow(t *testing.T) {
    baseURL := "http://localhost:8080"
    client := &http.Client{Timeout: 10 * time.Second}

    // 1. 注册
    resp, err := client.Post(baseURL+"/api/v1/register", "application/json",
        strings.NewReader(`{"email":"e2e@test.com","password":"secret"}`))
    require.NoError(t, err)
    assert.Equal(t, 201, resp.StatusCode)

    // 2. 登录
    // ...

    // 3. 核心操作
    // ...

    // 4. 清理
    // ...
}
```

运行方式：
```bash
# 启动服务
docker-compose up -d

# 跑 E2E
go test -tags=e2e ./test/e2e/...

# 清理
docker-compose down
```

## 四、契约测试

### 适用场景

- 有上游/下游 API 依赖时
- 协议字段变更时自动检测兼容性

### 模板

```go
// Contract: 验证 API 响应结构不意外变更
func TestContract_UserAPI(t *testing.T) {
    // 记录上次的响应结构（快照）
    expectedFields := []string{"id", "email", "name", "created_at"}

    handler := NewHandler(setupTestDB(t))
    router := setupRouter(handler)

    req := httptest.NewRequest("GET", "/api/v1/users/test-id", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    var resp map[string]interface{}
    json.Unmarshal(w.Body.Bytes(), &resp)

    for _, field := range expectedFields {
        _, exists := resp[field]
        assert.True(t, exists, "contract violation: missing field %q", field)
    }
}
```

### 反例免疫约束

```go
// WRONG: 改了 API 响应字段但没更新测试
// 删除了 user.age 字段，但契约测试不检查这个字段

// CORRECT: 契约测试覆盖所有承诺的字段
expectedFields := []string{"id", "email", "name", "created_at", "age"}
```
Why: 契约测试的价值就在于捕获意外变更。如果字段列表不完整，等于没有保护。

## 五、混沌测试

### 适用场景

- 系统依赖外部服务（DB、缓存、第三方 API）
- 需要验证降级和容错能力

### 模板

```go
func TestChaos_DBTimeout_ServiceDegradesGracefully(t *testing.T) {
    // 模拟 DB 超时
    slowDB, slowDBServer := setupSlowDB(t, 5*time.Second)
    defer slowDBServer.Close()

    handler := NewHandler(slowDB)
    router := setupRouter(handler)

    // 设置请求超时 < DB 超时
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    req := httptest.NewRequest("GET", "/api/v1/users/1", nil).WithContext(ctx)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    // 应该返回 503 而不是 500 或 hang 住
    assert.Equal(t, 503, w.Code)
}

func setupSlowDB(t *testing.T, delay time.Duration) (*sql.DB, *httptest.Server) {
    // 用 httptest.Server 模拟延迟
    // ...
}
```

### 混沌维度

| 注入类型 | 测试什么 | 模拟方式 |
|----------|----------|----------|
| 网络延迟 | 超时处理 | mock server 加延迟 |
| 服务不可用 | 降级策略 | mock server 返回 503 |
| 数据异常 | 输入校验 | 插入异常数据 |
| 并发竞争 | 并发安全 | 多 goroutine 并发读写 |

## 六、快照测试

### 适用场景

- API 响应结构变更检测
- 配置文件解析结果变更检测
- 模板渲染输出变更检测

### 模板

```go
func TestSnapshot_UserResponse(t *testing.T) {
    handler := NewHandler(setupTestDB(t))
    router := setupRouter(handler)

    req := httptest.NewRequest("GET", "/api/v1/users/test-id", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    // 格式化 JSON 保证稳定排序
    var buf bytes.Buffer
    json.Indent(&buf, w.Body.Bytes(), "", "  ")
    got := buf.String()

    // 读取快照
    snapshot := filepath.Join("testdata", "user_response_snapshot.json")
    if os.Getenv("UPDATE_SNAPSHOTS") != "" {
        os.WriteFile(snapshot, []byte(got), 0644)
        t.Log("snapshot updated")
        return
    }

    expected, err := os.ReadFile(snapshot)
    require.NoError(t, err)
    assert.Equal(t, string(expected), got)
}
```

更新快照：`UPDATE_SNAPSHOTS=1 go test ./...`

## 测试覆盖率门禁

| 模块类型 | 最低覆盖率 | 推荐覆盖率 |
|----------|-----------|-----------|
| 工具函数 | 90% | 95%+ |
| 业务逻辑 | 80% | 90%+ |
| HTTP Handler | 70% | 80%+ |
| 数据访问层 | 60% | 70%+ |

查看覆盖率：
```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out    # 按函数查看
go tool cover -html=coverage.out    # 浏览器查看
```

## 常见错误

| 错误 | 后果 | 预防 |
|------|------|------|
| 只写 happy path 测试 | 边界条件无覆盖 | 强制要求 error path 测试 |
| 测试之间有依赖 | 测试顺序敏感，偶尔失败 | 每个 Test 函数独立 setup/teardown |
| mock 和真实行为不一致 | 测试通过但线上失败 | 集成测试覆盖 mock 无法验证的部分 |
| 追求 100% 覆盖率 | 投入产出比下降 | 关注关键路径覆盖率，而非数字 |
| E2E 测试太多 | CI 时间爆炸 | E2E 只覆盖核心流程，不超过 10 个 |

## 验证清单

- [ ] 新模块有对应的测试策略（哪些用单元/集成/E2E）
- [ ] 单元测试使用表驱动
- [ ] 集成测试不依赖外部环境
- [ ] E2E 测试标记了 build tag
- [ ] 覆盖率 >= 80%
- [ ] 有 API 契约测试（如果有上下游依赖）
