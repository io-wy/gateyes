package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

const (
	authIdentityKey = "auth_identity"
	requestMetaKey  = "llm_request_meta"
)

type RequestMeta struct {
	Model           string
	EstimatedTokens int
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
