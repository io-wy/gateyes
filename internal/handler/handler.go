package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/gorilla/websocket"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
	"github.com/gateyes/gateway/internal/service/streaming"
)

type Handler struct {
	cfg     *config.Config
	deps    *Dependencies
	authSvc *auth.Auth
	metrics *Metrics
	logger  *slog.Logger
}

type Dependencies struct {
	Config      *config.Config
	APIKeyRepo  *repository.APIKeyRepository
	UserRepo    *repository.UserRepository
	Metrics     *Metrics
	ProviderMgr *provider.Manager
	KVCache     *cache.Cache
	Limiter     *limiter.Limiter
	Router      *router.Router
	Streaming   *streaming.Streaming
}

type Metrics struct {
	namespace string
	requests  *prometheus.CounterVec
	latency   *prometheus.HistogramVec
	tokens    *prometheus.CounterVec
	errors    *prometheus.CounterVec
	cacheHit  prometheus.Counter
	cacheMiss prometheus.Counter
}

func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		namespace: namespace,
		requests: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
		}, []string{"model", "status"}),
		latency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10},
		}, []string{"model"}),
		tokens: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "tokens_total",
		}, []string{"model", "type"}),
		errors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "errors_total",
		}, []string{"model", "type"}),
		cacheHit: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_hit_total",
		}),
		cacheMiss: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_miss_total",
		}),
	}
}

func NewHandler(deps *Dependencies) *Handler {
	return &Handler{
		cfg:     deps.Config,
		deps:    deps,
		authSvc: auth.NewAuth(deps.APIKeyRepo),
		metrics: deps.Metrics,
		logger:  slog.With("component", "handler"),
	}
}

// ============ Logging ============

func (h *Handler) logRequest(c *gin.Context, stage string, args ...any) {
	h.logger.Info(fmt.Sprintf("[%s] %s %s", stage, c.Request.Method, c.Request.URL.Path),
		append(args,
			"remote_addr", c.ClientIP(),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"query", c.Request.URL.Query().Encode(),
		)...)
}

func (h *Handler) logError(c *gin.Context, stage, err string, args ...any) {
	h.logger.Error(fmt.Sprintf("[%s] %s", stage, err),
		append(args,
			"remote_addr", c.ClientIP(),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
		)...)
}

// ============ Auth Middleware ============

func (h *Handler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key, secret := h.authSvc.ExtractKey(c.GetHeader("Authorization"))
		info, ok := h.authSvc.Verify(key, secret)

		h.logRequest(c, "AUTH",
			"api_key", key,
			"verified", ok,
		)

		if !ok {
			h.metrics.requests.WithLabelValues("", "unauthorized").Inc()
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "invalid API key",
					"type":    "invalid_request_error",
				},
			})
			c.Abort()
			return
		}

		c.Set("api_key", key)
		c.Set("api_key_info", info)
		c.Next()
	}
}

// ============ Chat Completions ============

func (h *Handler) Chat(c *gin.Context) {
	start := time.Now()
	var info *repository.APIKeyInfo

	// 1. 解析请求
	var req provider.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logError(c, "PARSE", err.Error())
		h.metrics.errors.WithLabelValues(req.Model, "invalid_request").Inc()
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": err.Error(),
				"type":   "invalid_request_error",
			},
		})
		return
	}

	key := c.GetString("api_key")
	if v, ok := c.Get("api_key_info"); ok {
		info, _ = v.(*repository.APIKeyInfo)
	}

	h.logRequest(c, "START",
		"api_key", key,
		"model", req.Model,
		"stream", req.Stream,
	)

	// 2. 模型检查
	if !h.authSvc.CheckModel(key, req.Model) {
		h.logError(c, "MODEL", "model not allowed", "model", req.Model)
		h.metrics.errors.WithLabelValues(req.Model, "model_not_allowed").Inc()
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"message": "model not allowed",
				"type":   "invalid_request_error",
			},
		})
		return
	}

	// 3. KV Cache 检查
	prompt := h.buildPrompt(req.Messages)
	if !req.Stream {
		if cached, ok := h.deps.KVCache.Get(prompt); ok {
			h.metrics.cacheHit.Inc()
			h.metrics.requests.WithLabelValues(req.Model, "cache_hit").Inc()
			h.logger.Info("[CACHE] hit", "api_key", key, "model", req.Model, "duration", time.Since(start))
			c.JSON(http.StatusOK, json.RawMessage(cached))
			return
		}
		h.metrics.cacheMiss.Inc()
	}

	// 4. 限流
	estimatedTokens := h.estimateTokens(prompt)
	if !h.deps.Limiter.Allow(c.Request.Context(), key, estimatedTokens) {
		h.metrics.requests.WithLabelValues(req.Model, "rate_limited").Inc()
		h.logError(c, "LIMITER", "rate limit exceeded", "tokens", estimatedTokens)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": "rate limit exceeded",
				"type":   "rate_limit_error",
			},
		})
		return
	}

	// 5. 路由选择
	p := h.deps.Router.Select(req.Model, c.GetHeader("X-Session-ID"))
	if p == nil {
		h.metrics.errors.WithLabelValues(req.Model, "no_provider").Inc()
		h.logError(c, "ROUTER", "no provider available")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"message": "no provider available",
				"type":   "internal_error",
			},
		})
		return
	}

	// 6. 转发请求
	h.deps.Router.IncLoad(p.Name)
	defer h.deps.Router.DecLoad(p.Name)

	h.logger.Info("[PROXY] to provider",
		"api_key", key,
		"model", req.Model,
		"provider", p.Name,
	)

	chatReq := &provider.ChatRequest{
		Model:    p.Model,
		Messages: req.Messages,
		Stream:   req.Stream,
	}

	if req.Stream {
		h.handleStream(c, p, chatReq, start)
	} else {
		h.handleNormal(c, p, chatReq, prompt, info, start)
	}
}

func (h *Handler) handleNormal(c *gin.Context, p *provider.Provider, req *provider.ChatRequest, prompt string, info *repository.APIKeyInfo, start time.Time) {
	resp, err := p.Chat(c.Request.Context(), req)
	if err != nil {
		h.logError(c, "UPSTREAM", err.Error(), "provider", p.Name)
		h.metrics.errors.WithLabelValues(p.Name, "upstream_error").Inc()

		// 传递上游错误
		errResp := h.buildErrorResponse(err, p.Name)
		c.JSON(http.StatusBadGateway, errResp)
		return
	}

	// 更新使用量
	if info != nil {
		h.deps.APIKeyRepo.Use(info.Key, resp.Usage.TotalTokens)
	}

	// 记录 metrics
	h.metrics.tokens.WithLabelValues(p.Name, "prompt").Add(float64(resp.Usage.PromptTokens))
	h.metrics.tokens.WithLabelValues(p.Name, "completion").Add(float64(resp.Usage.CompletionTokens))
	h.metrics.requests.WithLabelValues(p.Name, "success").Inc()

	// KV Cache 存储
	if h.cfg.Cache.Enabled {
		data, _ := json.Marshal(resp)
		h.deps.KVCache.Set(prompt, string(data))
	}

	h.metrics.latency.WithLabelValues(p.Name).Observe(time.Since(start).Seconds())

	h.logger.Info("[COMPLETE]",
		"provider", p.Name,
		"model", req.Model,
		"prompt_tokens", resp.Usage.PromptTokens,
		"completion_tokens", resp.Usage.CompletionTokens,
		"duration", time.Since(start),
	)

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) handleStream(c *gin.Context, p *provider.Provider, req *provider.ChatRequest, start time.Time) {
	stream, errCh := p.ChatStream(c.Request.Context(), req)

	// 检查错误
	select {
	case err := <-errCh:
		h.logError(c, "UPSTREAM", err.Error(), "provider", p.Name)
		h.metrics.errors.WithLabelValues(p.Name, "upstream_error").Inc()
		errResp := h.buildErrorResponse(err, p.Name)
		c.JSON(http.StatusBadGateway, errResp)
		return
	default:
	}

	h.deps.Streaming.ProxyChat(c, stream, errCh)
}

func (h *Handler) buildPrompt(messages []map[string]interface{}) string {
	var result string
	for _, m := range messages {
		if role, ok := m["role"].(string); ok {
			if content, ok := m["content"].(string); ok {
				result += role + ":" + content + "\n"
			}
		}
	}
	return result
}

func (h *Handler) estimateTokens(prompt string) int {
	return len(prompt) / 4
}

// ============ Responses API (OpenAI Compatible) ============

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (h *Handler) Responses(c *gin.Context) {
	// GET = WebSocket, POST = REST
	if c.Request.Method == "GET" {
		h.handleWebSocket(c)
		return
	}
	// Responses API 和 Chat Completions 格式相同，直接复用 Chat 处理
	h.Chat(c)
}

func (h *Handler) handleResponsesPOST(c *gin.Context) {
	start := time.Now()
	var info *repository.APIKeyInfo

	// 解析 OpenAI Responses API 请求
	var req provider.ResponsesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logError(c, "PARSE", err.Error())
		h.metrics.errors.WithLabelValues(req.Model, "invalid_request").Inc()
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": err.Error(),
				"type":   "invalid_request_error",
			},
		})
		return
	}

	key := c.GetString("api_key")
	if v, ok := c.Get("api_key_info"); ok {
		info, _ = v.(*repository.APIKeyInfo)
	}

	h.logRequest(c, "RESPONSES_START",
		"api_key", key,
		"model", req.Model,
		"stream", req.Stream,
	)

	// 模型检查
	if !h.authSvc.CheckModel(key, req.Model) {
		h.logError(c, "MODEL", "model not allowed", "model", req.Model)
		h.metrics.errors.WithLabelValues(req.Model, "model_not_allowed").Inc()
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"message": "model not allowed",
				"type":   "invalid_request_error",
			},
		})
		return
	}

	// KV Cache
	prompt := h.buildPromptFromResponse(req)
	if !req.Stream {
		if cached, ok := h.deps.KVCache.Get(prompt); ok {
			h.metrics.cacheHit.Inc()
			h.metrics.requests.WithLabelValues(req.Model, "cache_hit").Inc()
			h.logger.Info("[CACHE] hit", "api_key", key, "model", req.Model)
			c.JSON(http.StatusOK, json.RawMessage(cached))
			return
		}
		h.metrics.cacheMiss.Inc()
	}

	// 限流
	estimatedTokens := h.estimateTokens(prompt)
	if !h.deps.Limiter.Allow(c.Request.Context(), key, estimatedTokens) {
		h.metrics.requests.WithLabelValues(req.Model, "rate_limited").Inc()
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": "rate limit exceeded",
				"type":   "rate_limit_error",
			},
		})
		return
	}

	// 路由
	p := h.deps.Router.Select(req.Model, c.GetHeader("X-Session-ID"))
	if p == nil {
		h.metrics.errors.WithLabelValues(req.Model, "no_provider").Inc()
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"message": "no provider available",
				"type":   "internal_error",
			},
		})
		return
	}

	h.deps.Router.IncLoad(p.Name)
	defer h.deps.Router.DecLoad(p.Name)

	h.logger.Info("[RESPONSES] to provider",
		"api_key", key,
		"model", req.Model,
		"provider", p.Name,
	)

	// 转换为 Chat 格式
	chatReq := &provider.ChatRequest{
		Model:    p.Model,
		Messages: h.convertToChatMessages(req),
		Stream:   req.Stream,
	}

	if req.Stream {
		h.handleResponsesStream(c, p, chatReq, prompt, start)
	} else {
		h.handleResponsesNormal(c, p, chatReq, prompt, info, start)
	}
}

func (h *Handler) handleResponsesNormal(c *gin.Context, p *provider.Provider, req *provider.ChatRequest, prompt string, info *repository.APIKeyInfo, start time.Time) {
	resp, err := p.Chat(c.Request.Context(), req)
	if err != nil {
		h.logError(c, "UPSTREAM", err.Error(), "provider", p.Name)
		h.metrics.errors.WithLabelValues(p.Name, "upstream_error").Inc()
		errResp := h.buildErrorResponse(err, p.Name)
		c.JSON(http.StatusBadGateway, errResp)
		return
	}

	if info != nil {
		h.deps.APIKeyRepo.Use(info.Key, resp.Usage.TotalTokens)
	}

	h.metrics.tokens.WithLabelValues(p.Name, "prompt").Add(float64(resp.Usage.PromptTokens))
	h.metrics.tokens.WithLabelValues(p.Name, "completion").Add(float64(resp.Usage.CompletionTokens))
	h.metrics.requests.WithLabelValues(p.Name, "success").Inc()
	h.metrics.latency.WithLabelValues(p.Name).Observe(time.Since(start).Seconds())

	// 转换为 Responses API 格式
	response := provider.ConvertToResponses(resp)

	if h.cfg.Cache.Enabled {
		data, _ := json.Marshal(response)
		h.deps.KVCache.Set(prompt, string(data))
	}

	h.logger.Info("[RESPONSES_COMPLETE]",
		"provider", p.Name,
		"model", req.Model,
		"duration", time.Since(start),
	)

	c.JSON(http.StatusOK, response)
}

func (h *Handler) handleResponsesStream(c *gin.Context, p *provider.Provider, req *provider.ChatRequest, prompt string, start time.Time) {
	stream, errCh := p.ChatStream(c.Request.Context(), req)

	select {
	case err := <-errCh:
		h.logError(c, "UPSTREAM", err.Error(), "provider", p.Name)
		h.metrics.errors.WithLabelValues(p.Name, "upstream_error").Inc()
		errResp := h.buildErrorResponse(err, p.Name)
		c.JSON(http.StatusBadGateway, errResp)
		return
	default:
	}

	h.deps.Streaming.ProxyChat(c, stream, errCh)
}

func (h *Handler) handleWebSocket(c *gin.Context) {
	key := c.GetString("api_key")

	h.logger.Info("[WS] new connection",
		"api_key", key,
		"remote_addr", c.ClientIP(),
	)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logError(c, "WS_UPGRADE", err.Error())
		return
	}
	defer conn.Close()

	// 处理 WebSocket 消息
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err) {
				h.logger.Info("[WS] closed", "api_key", key, "error", err.Error())
			}
			break
		}

		// 解析请求
		var req provider.ResponsesRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			h.sendWSError(conn, err)
			continue
		}

		// 路由
		p := h.deps.Router.Select(req.Model, "")
		if p == nil {
			h.sendWSError(conn, fmt.Errorf("no provider available"))
			continue
		}

		h.deps.Router.IncLoad(p.Name)
		defer h.deps.Router.DecLoad(p.Name)

		// 转发
		chatReq := &provider.ChatRequest{
			Model:    p.Model,
			Messages: h.convertToChatMessages(req),
			Stream:   true,
		}

		stream, errCh := p.ChatStream(c.Request.Context(), chatReq)

		// 发送流式响应
		for {
			select {
			case data, ok := <-stream:
				if !ok {
					conn.WriteMessage(websocket.TextMessage, []byte(`{"done":true}`))
					return
				}
				conn.WriteMessage(websocket.TextMessage, []byte(data))
			case err := <-errCh:
				h.sendWSError(conn, err)
				return
			}
		}
	}
}

func (h *Handler) sendWSError(conn *websocket.Conn, err error) {
	data, _ := json.Marshal(gin.H{"error": err.Error()})
	conn.WriteMessage(websocket.TextMessage, data)
}

func (h *Handler) buildPromptFromResponse(req provider.ResponsesRequest) string {
	var result string
	for _, msg := range req.Messages {
		result += msg.Role + ": " + msg.Content + "\n"
	}
	return result
}

func (h *Handler) convertToChatMessages(req provider.ResponsesRequest) []map[string]interface{} {
	var messages []map[string]interface{}

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			msg.Role = "developer"
		}
		messages = append(messages, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	return messages
}

// ============ Error Response ============

func (h *Handler) buildErrorResponse(err error, provider string) gin.H {
	// 解析上游错误，传递下来
	errMsg := err.Error()

	// 判断错误类型
	errType := "upstream_error"
	errCode := 500

	if strings.Contains(errMsg, "timeout") {
		errType = "timeout_error"
		errCode = 504
	} else if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "authentication") {
		errType = "authentication_error"
		errCode = 401
	} else if strings.Contains(errMsg, "403") || strings.Contains(errMsg, "forbidden") {
		errType = "permission_error"
		errCode = 403
	} else if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "rate_limit") {
		errType = "rate_limit_error"
		errCode = 429
	} else if strings.Contains(errMsg, "400") || strings.Contains(errMsg, "invalid") {
		errType = "invalid_request_error"
		errCode = 400
	}

	return gin.H{
		"error": gin.H{
			"message":      errMsg,
			"type":         errType,
			"code":         errCode,
			"provider":     provider,
			"upstream_err": errMsg,
		},
	}
}

// ============ Models ============

func (h *Handler) Models(c *gin.Context) {
	providers := h.deps.ProviderMgr.List()
	var models []map[string]interface{}
	for _, p := range providers {
		models = append(models, map[string]interface{}{
			"id":         p.Model,
			"object":     "model",
			"created":    time.Now().Unix(),
			"owned_by":   p.Name,
			"provider":   p.Name,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": models})
}

func (h *Handler) Metrics(c *gin.Context) {
	http.DefaultServeMux.ServeHTTP(c.Writer, c.Request)
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) Ready(c *gin.Context) {
	if len(h.deps.ProviderMgr.List()) > 0 {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	} else {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no providers"})
	}
}

// ============ Server ============

var ErrServerClosed = fmt.Errorf("server closed")

type Server struct {
	addr   string
	engine *gin.Engine
}

func NewServer(cfg config.ServerConfig, h *Handler, adminH *AdminHandler) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(gin.Logger())

	// Debug endpoints
	engine.GET("/debug/pprof/*path", gin.WrapF(http.DefaultServeMux.ServeHTTP))

	// Health
	engine.GET("/health", h.Health)
	engine.GET("/ready", h.Ready)

	// Metrics
	engine.GET("/metrics", h.Metrics)

	// API v1
	v1 := engine.Group("/v1")
	v1.Use(h.AuthMiddleware())
	{
		v1.POST("/chat/completions", h.Chat)
		v1.POST("/responses", h.Responses)
		v1.GET("/responses", h.Responses) // WebSocket
		v1.GET("/models", h.Models)
	}

	// Admin API
	admin := engine.Group("/admin")
	admin.Use(adminH.AdminAuthMiddleware())
	{
		// Dashboard
		admin.GET("/dashboard", adminH.Dashboard)

		// Provider management
		admin.GET("/providers", adminH.GetProviders)
		admin.GET("/providers/:name", adminH.GetProvider)
		admin.GET("/providers/:name/stats", adminH.GetProviderStats)

		// User management
		admin.GET("/users", adminH.ListUsers)
		admin.POST("/users", adminH.CreateUser)
		admin.GET("/users/:id", adminH.GetUser)
		admin.PUT("/users/:id", adminH.UpdateUser)
		admin.DELETE("/users/:id", adminH.DeleteUser)
		admin.POST("/users/:id/reset", adminH.ResetUserUsage)
	}

	return &Server{
		addr:   cfg.ListenAddr,
		engine: engine,
	}
}

func (s *Server) Start() error {
	srv := &http.Server{
		Addr:         s.addr,
		Handler:      s.engine,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
	}
	return srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.addr,
		Handler: s.engine,
	}
	return srv.Shutdown(ctx)
}
