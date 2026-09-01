package public

import (
	"testing"

	"github.com/gin-gonic/gin"
)

type testHandler struct{}

func (testHandler) Health(*gin.Context)            {}
func (testHandler) Ready(*gin.Context)             {}
func (testHandler) Metrics(*gin.Context)           {}
func (testHandler) GetResponse(*gin.Context)       {}
func (testHandler) Models(*gin.Context)            {}
func (testHandler) CreateBatch(*gin.Context)       {}
func (testHandler) ListBatches(*gin.Context)       {}
func (testHandler) GetBatch(*gin.Context)          {}
func (testHandler) CancelBatch(*gin.Context)       {}
func (testHandler) ListBatchItems(*gin.Context)    {}
func (testHandler) Responses(*gin.Context)         {}
func (testHandler) Chat(*gin.Context)              {}
func (testHandler) AnthropicMessages(*gin.Context) {}
func (testHandler) Embeddings(*gin.Context)        {}
func (testHandler) ImageGenerations(*gin.Context)  {}
func (testHandler) ServiceResponses(*gin.Context)  {}
func (testHandler) ServiceChat(*gin.Context)       {}
func (testHandler) ServiceMessages(*gin.Context)   {}
func (testHandler) ServiceInvoke(*gin.Context)     {}

func TestRegisterRoutes_PreservesPublicSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	RegisterRoutes(e, testHandler{}, nil)
	want := map[string]bool{
		"GET /health": true, "GET /ready": true, "GET /metrics": true,
		"GET /v1/models": true, "POST /v1/responses": true,
		"POST /v1/chat/completions": true, "POST /v1/messages": true,
		"POST /v1/embeddings": true, "POST /v1/images/generations": true,
		"POST /service/:prefix/invoke": true,
	}
	got := map[string]bool{}
	for _, r := range e.Routes() {
		got[r.Method+" "+r.Path] = true
	}
	for route := range want {
		if !got[route] {
			t.Errorf("missing route %s", route)
		}
	}
}
