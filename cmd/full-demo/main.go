package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gateyes/gateway/internal/ingress"
	"github.com/gateyes/gateway/internal/proxy"
)

// full-demo 同时演示 AI 网关 + Ingress 微服务网关双模式。
// 零依赖，一条命令跑起来：go run ./cmd/full-demo
//
// 架构：
//   Request ──► [IngressMiddleware] ──命中?──► ReverseProxy ──► 假上游(:18080)
//                       │
//                       未命中
//                       ▼
//                AI Mock Handler (/v1/*, /admin/*)

func main() {
	gin.SetMode(gin.ReleaseMode)

	// 1. 启动假上游服务
	upstream := startFakeUpstream(18080)
	defer upstream.Close()
	time.Sleep(200 * time.Millisecond)

	// 2. 构建 Ingress 路由表
	rt := ingress.NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			ID:          "demo-api",
			Host:        "",
			Path:        "/api",
			PathType:    proxy.PathTypePrefix,
			BackendPool: pool("upstream", "127.0.0.1:18080", "http", 1),
			Annotations: &proxy.Annotations{},
		},
		{
			ID:          "demo-rewrite",
			Host:        "",
			Path:        "/rewrite",
			PathType:    proxy.PathTypePrefix,
			BackendPool: pool("upstream", "127.0.0.1:18080", "http", 1),
			Annotations: &proxy.Annotations{RewriteTarget: "/v2"},
		},
		{
			ID:          "demo-cors",
			Host:        "",
			Path:        "/cors",
			PathType:    proxy.PathTypePrefix,
			BackendPool: pool("upstream", "127.0.0.1:18080", "http", 1),
			Annotations: &proxy.Annotations{
				EnableCORS:       true,
				CORSAllowOrigin:  []string{"*"},
				CORSAllowMethods: []string{"GET", "POST", "OPTIONS"},
				CORSAllowHeaders: []string{"Content-Type", "Authorization"},
			},
		},
		{
			ID:          "demo-ratelimit",
			Host:        "",
			Path:        "/ratelimit",
			PathType:    proxy.PathTypePrefix,
			BackendPool: pool("upstream", "127.0.0.1:18080", "http", 1),
			Annotations: &proxy.Annotations{RateLimitRPS: 2},
		},
		{
			ID:          "demo-canary",
			Host:        "",
			Path:        "/canary",
			PathType:    proxy.PathTypePrefix,
			BackendPool: pool("stable", "127.0.0.1:18080", "http", 1),
			CanaryBackendPool: pool("canary", "127.0.0.1:18080", "http", 1),
			Annotations: &proxy.Annotations{Canary: true, CanaryWeight: 20},
		},
		{
			ID:          "demo-host",
			Host:        "app.local",
			Path:        "/api",
			PathType:    proxy.PathTypePrefix,
			BackendPool: pool("upstream", "127.0.0.1:18080", "http", 1),
			Annotations: &proxy.Annotations{},
		},
	})

	// 3. 创建 Ingress middleware
	p := proxy.NewProxy(proxy.DefaultProxyConfig())
	ingressMW := ingress.NewMiddleware(ingress.MiddlewareOpts{
		RouteTable: rt,
		Proxy:      p,
		Enabled:    true,
	})

	// 4. 创建 Gin 引擎
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(ingressMW.Handler()) // Ingress 在最前拦截

	// AI 网关路由（Ingress 未命中时落到这儿）
	engine.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	engine.GET("/ready", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ready"}) })
	engine.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := engine.Group("/v1")
	{
		v1.GET("/models", mockModels)
		v1.POST("/chat/completions", mockChat)
		v1.POST("/responses", mockResponses)
	}

	admin := engine.Group("/admin")
	{
		admin.GET("/tenants", mockListTenants)
		admin.POST("/tenants", mockCreateTenant)
		admin.GET("/users", mockListUsers)
		admin.POST("/users", mockCreateUser)
		admin.GET("/keys", mockListKeys)
	}

	// 兜底 404
	engine.NoRoute(func(c *gin.Context) {
		c.JSON(404, gin.H{"error": "not found", "path": c.Request.URL.Path})
	})

	// 5. 启动
	addr := ":8028"
	log.Printf("=== Gateyes Full Demo 启动 ===")
	log.Printf("监听: http://127.0.0.1%s", addr)
	log.Printf("假上游: http://127.0.0.1:18080")
	printCommands()

	if err := engine.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// ---------- 假上游 ----------

func startFakeUpstream(port int) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"upstream":"fake","method":"%s","path":"%s","host":"%s"}`+"\n",
			r.Method, r.URL.Path, r.Host)
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"upstream":"fake","version":"v2","method":"%s","path":"%s"}`+"\n",
			r.Method, r.URL.Path)
	})
	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	go srv.ListenAndServe()
	return srv
}

func pool(name, addr, proto string, weight int) *proxy.BackendPool {
	return proxy.NewBackendPool([]proxy.Backend{
		proxy.NewBackend(name, addr, proto, weight),
	})
}

// ---------- AI Mock Handlers ----------

func mockModels(c *gin.Context) {
	c.JSON(200, gin.H{
		"object": "list",
		"data": []gin.H{
			{"id": "gpt-4o-mini", "object": "model"},
			{"id": "claude-3-5-sonnet", "object": "model"},
		},
	})
}

func mockChat(c *gin.Context) {
	var body struct {
		Stream bool `json:"stream"`
		Model  string `json:"model"`
	}
	_ = c.BindJSON(&body)

	if body.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Stream(func(w io.Writer) bool {
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"hello"}}]}`)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			return false
		})
		return
	}

	c.JSON(200, gin.H{
		"id":      "chatcmpl-demo",
		"object":  "chat.completion",
		"model":   body.Model,
		"choices": []gin.H{{"message": gin.H{"role": "assistant", "content": "Hello from Gateyes mock!"}}},
		"usage":   gin.H{"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18},
	})
}

func mockResponses(c *gin.Context) {
	c.JSON(200, gin.H{
		"id":     "resp-demo",
		"status": "completed",
		"output": []gin.H{{"type": "message", "content": []gin.H{{"type": "text", "text": "Mock response"}}}},
	})
}

// ---------- Admin Mock Handlers ----------

func mockListTenants(c *gin.Context) {
	c.JSON(200, gin.H{"data": []gin.H{{"slug": "default", "name": "Default Tenant"}}})
}

func mockCreateTenant(c *gin.Context) {
	c.JSON(201, gin.H{"slug": "new-team", "name": "New Team", "api_key": "demo-key", "api_secret": "demo-secret"})
}

func mockListUsers(c *gin.Context) {
	c.JSON(200, gin.H{"data": []gin.H{{"id": 1, "name": "alice", "role": "tenant_user"}}})
}

func mockCreateUser(c *gin.Context) {
	c.JSON(201, gin.H{"id": 2, "name": "bob", "api_key": "user-key-002", "api_secret": "user-secret-002"})
}

func mockListKeys(c *gin.Context) {
	c.JSON(200, gin.H{"data": []gin.H{{"id": 1, "key": "test-key-001", "quota": 1000000}}})
}

// ---------- 输出 ----------

func printCommands() {
	fmt.Println("")
	fmt.Println("══════════════ AI 网关测试 ══════════════")
	fmt.Println("")
	fmt.Println("【1】获取模型列表")
	fmt.Println("  curl http://127.0.0.1:8028/v1/models")
	fmt.Println("")
	fmt.Println("【2】Chat Completions（非流式）")
	fmt.Println("  curl -X POST http://127.0.0.1:8028/v1/chat/completions \\")
	fmt.Println("    -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"model\":\"gpt-4o-mini\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'")
	fmt.Println("")
	fmt.Println("【3】Chat Completions（流式）")
	fmt.Println("  curl -X POST http://127.0.0.1:8028/v1/chat/completions \\")
	fmt.Println("    -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"model\":\"gpt-4o-mini\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'")
	fmt.Println("")
	fmt.Println("【4】Admin - 创建租户")
	fmt.Println("  curl -X POST http://127.0.0.1:8028/admin/tenants \\")
	fmt.Println("    -H 'Authorization: Bearer admin-key-001:admin-secret-001' \\")
	fmt.Println("    -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"slug\":\"my-team\",\"name\":\"My Team\"}'")
	fmt.Println("")
	fmt.Println("【5】健康检查")
	fmt.Println("  curl http://127.0.0.1:8028/health")
	fmt.Println("  curl http://127.0.0.1:8028/ready")
	fmt.Println("")
	fmt.Println("【6】Prometheus 指标")
	fmt.Println("  curl -s http://127.0.0.1:8028/metrics | grep -E 'gateway_|ingress_'")
	fmt.Println("")
	fmt.Println("══════════════ Ingress 测试 ══════════════")
	fmt.Println("")
	fmt.Println("【7】基础代理  /api/* → 假上游")
	fmt.Println("  curl http://127.0.0.1:8028/api/hello")
	fmt.Println("")
	fmt.Println("【8】路径重写  /rewrite/v1/users → /v2/v1/users")
	fmt.Println("  curl http://127.0.0.1:8028/rewrite/v1/users")
	fmt.Println("")
	fmt.Println("【9】CORS 预检")
	fmt.Println("  curl -X OPTIONS -H 'Origin: http://example.com' -H 'Access-Control-Request-Method: POST' http://127.0.0.1:8028/cors/data")
	fmt.Println("")
	fmt.Println("【10】限流  /ratelimit/* RPS=2，第11个起429")
	fmt.Println("  for i in $(seq 1 12); do curl -s -o /dev/null -w '%{http_code} ' http://127.0.0.1:8028/ratelimit/hit; done; echo")
	fmt.Println("")
	fmt.Println("【11】Host 路由  app.local/api/*")
	fmt.Println("  curl -H 'Host: app.local' http://127.0.0.1:8028/api/info")
	fmt.Println("")
	fmt.Println("【12】兜底路由（未命中 Ingress）")
	fmt.Println("  curl http://127.0.0.1:8028/v1/chat/completions")
	fmt.Println("")
	fmt.Println("按 Ctrl+C 停止")
	fmt.Println("")
}
