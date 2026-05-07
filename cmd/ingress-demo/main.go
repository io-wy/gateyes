package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/ingress"
	"github.com/gateyes/gateway/internal/proxy"
)

// ingress-demo 是一个零依赖的本地演示程序，
// 展示 Gateyes Ingress Controller 的核心能力，无需 K8s 集群。
//
// 用法:
//
//	go run ./cmd/ingress-demo
//
// 然后按终端输出的 curl 命令逐一测试。

func main() {
	gin.SetMode(gin.ReleaseMode)

	// 启动 4 个假上游服务
	apiV1 := startFakeUpstream(18081, "api-v1")
	apiV2 := startFakeUpstream(18082, "api-v2")
	canarySvc := startFakeUpstream(18083, "canary")
	stableSvc := startFakeUpstream(18084, "stable")
	defer apiV1.Close()
	defer apiV2.Close()
	defer canarySvc.Close()
	defer stableSvc.Close()

	// 等待上游就绪
	time.Sleep(200 * time.Millisecond)

	// 构建路由表（模拟 K8s Ingress reconcile 的结果）
	routeTable := ingress.NewRouteTable()
	routeTable.Replace([]proxy.RouteRule{
		// 1. 基础代理: /api/* -> api-v1
		{
			ID:          "demo-api",
			Host:        "",
			Path:        "/api",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("api-v1", "127.0.0.1:18081", "http", 1),
			}),
			Annotations: &proxy.Annotations{},
		},
		// 2. 路径重写: /rewrite/v1/users -> /v2/users
		{
			ID:          "demo-rewrite",
			Host:        "",
			Path:        "/rewrite",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("api-v2", "127.0.0.1:18082", "http", 1),
			}),
			Annotations: &proxy.Annotations{
				RewriteTarget: "/v2",
			},
		},
		// 3. CORS 支持: /cors/*
		{
			ID:          "demo-cors",
			Host:        "",
			Path:        "/cors",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("api-v1", "127.0.0.1:18081", "http", 1),
			}),
			Annotations: &proxy.Annotations{
				EnableCORS:       true,
				CORSAllowOrigin:  []string{"*"},
				CORSAllowMethods: []string{"GET", "POST", "OPTIONS"},
				CORSAllowHeaders: []string{"Content-Type", "Authorization"},
			},
		},
		// 4. 限流: /ratelimit/* (RPS=2)
		{
			ID:          "demo-ratelimit",
			Host:        "",
			Path:        "/ratelimit",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("api-v1", "127.0.0.1:18081", "http", 1),
			}),
			Annotations: &proxy.Annotations{
				RateLimitRPS: 2,
			},
		},
		// 5. 金丝雀: /canary/* -> 80% stable + 20% canary
		{
			ID:          "demo-canary",
			Host:        "",
			Path:        "/canary",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("stable", "127.0.0.1:18084", "http", 1),
			}),
			CanaryBackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("canary", "127.0.0.1:18083", "http", 1),
			}),
			Annotations: &proxy.Annotations{
				Canary:       true,
				CanaryWeight: 20,
			},
		},
		// 6. Header 强切金丝雀: /canary-header/*
		{
			ID:          "demo-canary-header",
			Host:        "",
			Path:        "/canary-header",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("stable", "127.0.0.1:18084", "http", 1),
			}),
			CanaryBackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("canary", "127.0.0.1:18083", "http", 1),
			}),
			Annotations: &proxy.Annotations{
				Canary:         true,
				CanaryByHeader: "X-Canary",
			},
		},
		// 7. IP 白名单: /admin/* (仅 127.0.0.1)
		{
			ID:          "demo-whitelist",
			Host:        "",
			Path:        "/admin",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("api-v1", "127.0.0.1:18081", "http", 1),
			}),
			Annotations: &proxy.Annotations{
				WhitelistSourceRange: []string{"127.0.0.1/32"},
			},
		},
		// 8. Host 匹配: app.local/api/*
		{
			ID:          "demo-host",
			Host:        "app.local",
			Path:        "/api",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{
				proxy.NewBackend("api-v2", "127.0.0.1:18082", "http", 1),
			}),
			Annotations: &proxy.Annotations{},
		},
	})

	// 创建 proxy 和 ingress middleware
	p := proxy.NewProxy(proxy.DefaultProxyConfig())
	ingressMW := ingress.NewMiddleware(ingress.MiddlewareOpts{
		RouteTable: routeTable,
		Proxy:      p,
		Enabled:    true,
	})

	// 创建 Gin 引擎：ingress 中间件在最前
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(ingressMW.Handler())

	// 兜底：未命中 ingress 路由的请求返回提示
	engine.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"message": "未命中 ingress 路由，这是 AI 网关兜底响应",
			"path":    c.Request.URL.Path,
		})
	})

	// 健康检查
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 启动
	addr := ":8083"
	log.Printf("Gateyes Ingress Demo 启动于 http://127.0.0.1%s", addr)
	printCurlCommands()

	if err := engine.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// startFakeUpstream 启动一个返回标识信息的假上游 HTTP 服务。
func startFakeUpstream(port int, name string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"upstream":"%s","method":"%s","path":"%s","host":"%s"}`+"\n",
			name, r.Method, r.URL.Path, r.Host)
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"upstream":"%s","version":"v2","method":"%s","path":"%s"}`+"\n",
			name, r.Method, r.URL.Path)
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	go srv.ListenAndServe()
	return srv
}

func printCurlCommands() {
	fmt.Println("")
	fmt.Println("=== Gateyes Ingress Controller 本地演示 ===")
	fmt.Println("")
	fmt.Println("【1】基础代理 —— /api/* 转发到 api-v1")
	fmt.Println("  curl http://127.0.0.1:8083/api/hello")
	fmt.Println("")
	fmt.Println("【2】路径重写 —— /rewrite/v1/users → /v2/users")
	fmt.Println("  curl http://127.0.0.1:8083/rewrite/v1/users")
	fmt.Println("")
	fmt.Println("【3】CORS 预检 —— /cors/* 自动响应 OPTIONS")
	fmt.Println("  curl -X OPTIONS -H 'Origin: http://example.com' -H 'Access-Control-Request-Method: POST' http://127.0.0.1:8083/cors/data")
	fmt.Println("  curl -H 'Origin: http://example.com' http://127.0.0.1:8083/cors/data")
	fmt.Println("")
	fmt.Println("【4】限流 —— /ratelimit/* RPS=2，令牌桶突发容量=10，第 11 个起返回 429")
	fmt.Println("  for i in 1 2 3; do curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8083/ratelimit/hit; done")
	fmt.Println("")
	fmt.Println("【5】金丝雀权重 —— /canary/* 20% 流量到 canary")
	fmt.Println("  for i in $(seq 1 10); do curl -s http://127.0.0.1:8083/canary/roll; done | sort | uniq -c")
	fmt.Println("")
	fmt.Println("【6】Header 强切金丝雀 —— X-Canary: always 100% 到 canary")
	fmt.Println("  curl -H 'X-Canary: always' http://127.0.0.1:8083/canary-header/force")
	fmt.Println("")
	fmt.Println("【7】IP 白名单 —— /admin/* 仅允许 127.0.0.1")
	fmt.Println("  curl http://127.0.0.1:8083/admin/dashboard")
	fmt.Println("")
	fmt.Println("【8】Host 路由 —— app.local/api/* 转发到 api-v2")
	fmt.Println("  curl -H 'Host: app.local' http://127.0.0.1:8083/api/info")
	fmt.Println("")
	fmt.Println("【9】未命中路由 —— 返回 AI 网关兜底")
	fmt.Println("  curl http://127.0.0.1:8083/v1/chat/completions")
	fmt.Println("")
	fmt.Println("【10】Prometheus 指标")
	fmt.Println("  curl -s http://127.0.0.1:8083/metrics | grep ingress_")
	fmt.Println("")
	fmt.Println("按 Ctrl+C 停止")
	fmt.Println("")
}
