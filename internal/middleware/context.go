package middleware

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

const (
	authIdentityKey   = "auth_identity"
	requestMetaKey    = "llm_request_meta"
	requestContextKey = "request_correlation"
	RequestIDHeader   = "X-Request-ID"
	TraceparentHeader = "traceparent"
)

type requestContextKeyType struct{}

var requestContextValueKey requestContextKeyType

type RequestMeta struct {
	Model           string
	EstimatedTokens int
}

type RequestContext struct {
	RequestID   string
	TraceID     string
	Traceparent string
}

func SetIdentity(c *gin.Context, identity *repository.AuthIdentity) {
	c.Set(authIdentityKey, identity)
}

func Identity(c *gin.Context) (*repository.AuthIdentity, bool) {
	value, ok := c.Get(authIdentityKey)
	if !ok {
		return nil, false
	}

	identity, ok := value.(*repository.AuthIdentity)
	return identity, ok
}

func SetRequestMeta(c *gin.Context, meta *RequestMeta) {
	c.Set(requestMetaKey, meta)
}

func GetRequestMeta(c *gin.Context) (*RequestMeta, bool) {
	value, ok := c.Get(requestMetaKey)
	if !ok {
		return nil, false
	}

	meta, ok := value.(*RequestMeta)
	return meta, ok
}

func SetRequestContext(c *gin.Context, requestCtx *RequestContext) {
	c.Set(requestContextKey, requestCtx)
	if requestCtx != nil && c.Request != nil {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), requestContextValueKey, requestCtx))
	}
}

func GetRequestContext(c *gin.Context) (*RequestContext, bool) {
	value, ok := c.Get(requestContextKey)
	if !ok {
		return nil, false
	}

	requestCtx, ok := value.(*RequestContext)
	return requestCtx, ok
}

func RequestContextFromContext(ctx context.Context) (*RequestContext, bool) {
	if ctx == nil {
		return nil, false
	}
	requestCtx, ok := ctx.Value(requestContextValueKey).(*RequestContext)
	return requestCtx, ok
}
