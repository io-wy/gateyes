package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

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
	Store       repository.Store
	Metrics     *Metrics
	ProviderMgr *provider.Manager
	KVCache     *cache.Cache
	Limiter     *limiter.Limiter
	Router      *router.Router
	Streaming   *streaming.Streaming
}

type Metrics struct {
	requests  *prometheus.CounterVec
	latency   *prometheus.HistogramVec
	tokens    *prometheus.CounterVec
	errors    *prometheus.CounterVec
	cacheHit  prometheus.Counter
	cacheMiss prometheus.Counter
}

func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		requests: promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "requests_total"}, []string{"model", "status"}),
		latency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10},
		}, []string{"model"}),
		tokens:    promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "tokens_total"}, []string{"model", "type"}),
		errors:    promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "errors_total"}, []string{"model", "type"}),
		cacheHit:  promauto.NewCounter(prometheus.CounterOpts{Namespace: namespace, Name: "cache_hit_total"}),
		cacheMiss: promauto.NewCounter(prometheus.CounterOpts{Namespace: namespace, Name: "cache_miss_total"}),
	}
}

func NewHandler(deps *Dependencies) *Handler {
	return &Handler{
		cfg:     deps.Config,
		deps:    deps,
		authSvc: auth.NewAuth(deps.Store),
		metrics: deps.Metrics,
		logger:  slog.With("component", "handler"),
	}
}

func (h *Handler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key, secret := h.authSvc.ExtractKey(c.GetHeader("Authorization"))
		identity, err := h.authSvc.Authenticate(c.Request.Context(), key, secret)
		if err != nil {
			h.metrics.requests.WithLabelValues("", "unauthorized").Inc()
			status := http.StatusUnauthorized
			message := "invalid API key"
			if errors.Is(err, auth.ErrInactiveAPIKey) {
				status = http.StatusForbidden
				message = "inactive API key"
			}
			c.JSON(status, gin.H{"error": gin.H{"message": message, "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		c.Set("auth_identity", identity)
		c.Next()
	}
}

func (h *Handler) Chat(c *gin.Context) {
	start := time.Now()

	var req provider.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.errors.WithLabelValues(req.Model, "invalid_request").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := h.authIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	if !h.authSvc.CheckModel(identity, req.Model) {
		h.metrics.errors.WithLabelValues(req.Model, "model_not_allowed").Inc()
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": auth.ErrModelNotAllowed.Error(), "type": "invalid_request_error"}})
		return
	}

	cacheKey := h.buildCacheKey(req.Model, req.Messages)
	if !req.Stream && h.cfg.Cache.Enabled {
		if cached, ok := h.deps.KVCache.Get(cacheKey); ok {
			h.metrics.cacheHit.Inc()
			h.metrics.requests.WithLabelValues(req.Model, "cache_hit").Inc()
			c.JSON(http.StatusOK, json.RawMessage(cached))
			return
		}
		h.metrics.cacheMiss.Inc()
	}

	estimatedTokens := h.estimateTokens(req.Messages)
	if !h.authSvc.HasQuota(identity, estimatedTokens) {
		h.metrics.requests.WithLabelValues(req.Model, "quota_exceeded").Inc()
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": auth.ErrQuotaExceeded.Error(), "type": "rate_limit_error"}})
		return
	}

	if !h.deps.Limiter.Allow(c.Request.Context(), identity.APIKey, estimatedTokens) {
		h.metrics.requests.WithLabelValues(req.Model, "rate_limited").Inc()
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "rate limit exceeded", "type": "rate_limit_error"}})
		return
	}

	selected, err := h.selectProvider(c.Request.Context(), identity, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.metrics.errors.WithLabelValues(req.Model, "provider_filter_error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}
	if selected == nil {
		h.metrics.errors.WithLabelValues(req.Model, "no_provider").Inc()
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no provider available", "type": "internal_error"}})
		return
	}

	upstreamReq := &provider.ChatRequest{
		Model:     selected.Model(),
		Messages:  req.Messages,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
	}

	h.deps.Router.IncLoad(selected.Name())
	h.deps.ProviderMgr.Stats.IncrementLoad(selected.Name())
	defer func() {
		h.deps.Router.DecLoad(selected.Name())
		h.deps.ProviderMgr.Stats.DecrementLoad(selected.Name())
	}()

	if req.Stream {
		h.handleStream(c, identity, selected, upstreamReq)
		return
	}

	h.handleNormal(c, identity, selected, upstreamReq, req.Model, cacheKey, start)
}

func (h *Handler) Responses(c *gin.Context) {
	h.handleResponsesCreate(c)
}

func (h *Handler) Models(c *gin.Context) {
	identity, ok := h.authIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	providers, err := h.allowedProviders(c.Request.Context(), identity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}
	models := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		models = append(models, map[string]any{
			"id":       p.Model(),
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": p.Name(),
			"provider": p.Name(),
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
	if len(h.deps.ProviderMgr.List()) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no providers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func (h *Handler) handleNormal(c *gin.Context, identity *repository.AuthIdentity, p provider.Provider, req *provider.ChatRequest, requestedModel, cacheKey string, start time.Time) {
	resp, err := p.Chat(c.Request.Context(), req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		h.deps.ProviderMgr.Stats.RecordRequest(p.Name(), false, 0, latencyMs)
		h.metrics.errors.WithLabelValues(p.Name(), "upstream_error").Inc()
		_ = h.authSvc.RecordUsage(c.Request.Context(), identity, p.Name(), requestedModel, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
		c.JSON(http.StatusBadGateway, h.buildErrorResponse(err, p.Name()))
		return
	}

	if err := h.authSvc.RecordUsage(
		c.Request.Context(),
		identity,
		p.Name(),
		requestedModel,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		p.Cost(resp.Usage.PromptTokens, resp.Usage.CompletionTokens),
		latencyMs,
		"success",
		"",
	); err != nil {
		if errors.Is(err, auth.ErrQuotaExceeded) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": auth.ErrQuotaExceeded.Error(), "type": "rate_limit_error"}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}

	h.deps.ProviderMgr.Stats.RecordRequest(p.Name(), true, resp.Usage.TotalTokens, latencyMs)
	h.metrics.tokens.WithLabelValues(p.Name(), "prompt").Add(float64(resp.Usage.PromptTokens))
	h.metrics.tokens.WithLabelValues(p.Name(), "completion").Add(float64(resp.Usage.CompletionTokens))
	h.metrics.requests.WithLabelValues(p.Name(), "success").Inc()
	h.metrics.latency.WithLabelValues(p.Name()).Observe(time.Since(start).Seconds())

	if h.cfg.Cache.Enabled {
		data, _ := json.Marshal(resp)
		h.deps.KVCache.Set(cacheKey, string(data))
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) handleStream(c *gin.Context, identity *repository.AuthIdentity, p provider.Provider, req *provider.ChatRequest) {
	if err := h.authSvc.Touch(c.Request.Context(), identity); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}

	stream, errCh := p.ChatStream(c.Request.Context(), req)
	select {
	case err := <-errCh:
		if err != nil {
			h.deps.ProviderMgr.Stats.RecordRequest(p.Name(), false, 0, 0)
			_ = h.authSvc.RecordUsage(c.Request.Context(), identity, p.Name(), req.Model, 0, 0, 0, 0, 0, "error", "upstream_error")
			c.JSON(http.StatusBadGateway, h.buildErrorResponse(err, p.Name()))
			return
		}
	default:
	}

	h.deps.Streaming.ProxyChat(c, stream, errCh)
}

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (h *Handler) handleWebSocket(c *gin.Context) {
	identity, ok := h.authIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	if err := h.authSvc.Touch(c.Request.Context(), identity); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var req provider.ResponsesRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			h.sendWSError(conn, err)
			continue
		}

		if !h.authSvc.CheckModel(identity, req.Model) {
			h.sendWSError(conn, auth.ErrModelNotAllowed)
			continue
		}

		selected := h.deps.Router.Select(req.Model, "")
		if selected == nil {
			h.sendWSError(conn, fmt.Errorf("no provider available"))
			continue
		}

		upstreamReq := &provider.ChatRequest{
			Model:    selected.Model(),
			Messages: req.Messages,
			Stream:   true,
		}
		stream, errCh := selected.ChatStream(c.Request.Context(), upstreamReq)

		for {
			select {
			case data, ok := <-stream:
				if !ok {
					conn.WriteMessage(websocket.TextMessage, []byte(`{"done":true}`))
					goto nextRequest
				}
				conn.WriteMessage(websocket.TextMessage, []byte(data))
			case err := <-errCh:
				if err != nil {
					h.sendWSError(conn, err)
				}
				goto nextRequest
			}
		}
	nextRequest:
	}
}

func (h *Handler) sendWSError(conn *websocket.Conn, err error) {
	data, _ := json.Marshal(gin.H{"error": err.Error()})
	conn.WriteMessage(websocket.TextMessage, data)
}

func (h *Handler) buildCacheKey(model string, messages []provider.ChatMessage) string {
	var b strings.Builder
	b.WriteString(model)
	b.WriteString("\n")
	for _, message := range messages {
		b.WriteString(message.Role)
		b.WriteString(":")
		b.WriteString(message.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func (h *Handler) estimateTokens(messages []provider.ChatMessage) int {
	var total int
	for _, message := range messages {
		total += len(message.Content) / 4
	}
	if total == 0 {
		return 1
	}
	return total
}

func (h *Handler) buildErrorResponse(err error, providerName string) gin.H {
	errMsg := err.Error()
	errType := "upstream_error"
	errCode := http.StatusBadGateway

	switch {
	case strings.Contains(errMsg, "timeout"):
		errType = "timeout_error"
		errCode = http.StatusGatewayTimeout
	case strings.Contains(errMsg, "401"), strings.Contains(errMsg, "authentication"):
		errType = "authentication_error"
		errCode = http.StatusUnauthorized
	case strings.Contains(errMsg, "403"), strings.Contains(errMsg, "forbidden"):
		errType = "permission_error"
		errCode = http.StatusForbidden
	case strings.Contains(errMsg, "429"), strings.Contains(errMsg, "rate_limit"):
		errType = "rate_limit_error"
		errCode = http.StatusTooManyRequests
	case strings.Contains(errMsg, "400"), strings.Contains(errMsg, "invalid"):
		errType = "invalid_request_error"
		errCode = http.StatusBadRequest
	}

	return gin.H{"error": gin.H{
		"message":      errMsg,
		"type":         errType,
		"code":         errCode,
		"provider":     providerName,
		"upstream_err": errMsg,
	}}
}

func (h *Handler) authIdentity(c *gin.Context) (*repository.AuthIdentity, bool) {
	value, ok := c.Get("auth_identity")
	if !ok {
		return nil, false
	}
	identity, ok := value.(*repository.AuthIdentity)
	return identity, ok
}

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

	engine.GET("/debug/pprof/*path", gin.WrapF(http.DefaultServeMux.ServeHTTP))
	engine.GET("/health", h.Health)
	engine.GET("/ready", h.Ready)
	engine.GET("/metrics", h.Metrics)

	v1 := engine.Group("/v1")
	v1.Use(h.AuthMiddleware())
	{
		v1.POST("/chat/completions", h.Chat)
		v1.POST("/responses", h.Responses)
		v1.GET("/responses/:id", h.GetResponse)
		v1.GET("/models", h.Models)
	}

	admin := engine.Group("/admin")
	admin.Use(h.AuthMiddleware())
	admin.Use(adminH.RequireRoles(repository.RoleTenantAdmin, repository.RoleSuperAdmin))
	{
		admin.GET("/dashboard", adminH.Dashboard)
		admin.GET("/providers", adminH.GetProviders)
		admin.GET("/providers/:name", adminH.GetProvider)
		admin.GET("/providers/:name/stats", adminH.GetProviderStats)
		admin.GET("/users", adminH.ListUsers)
		admin.POST("/users", adminH.CreateUser)
		admin.GET("/users/:id", adminH.GetUser)
		admin.PUT("/users/:id", adminH.UpdateUser)
		admin.DELETE("/users/:id", adminH.DeleteUser)
		admin.POST("/users/:id/reset", adminH.ResetUserUsage)
	}

	tenants := admin.Group("/tenants")
	tenants.Use(adminH.RequireRoles(repository.RoleSuperAdmin))
	{
		tenants.GET("", adminH.ListTenants)
		tenants.POST("", adminH.CreateTenant)
		tenants.GET("/:id", adminH.GetTenant)
		tenants.PUT("/:id", adminH.UpdateTenant)
		tenants.POST("/:id/providers", adminH.ReplaceTenantProviders)
	}

	return &Server{addr: cfg.ListenAddr, engine: engine}
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
	srv := &http.Server{Addr: s.addr, Handler: s.engine}
	return srv.Shutdown(ctx)
}
