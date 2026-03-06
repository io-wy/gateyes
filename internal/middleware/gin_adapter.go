package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ToGinMiddleware adapts existing net/http middleware to gin middleware.
func ToGinMiddleware(mw Middleware) gin.HandlerFunc {
	if mw == nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		nextCalled := false

		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.Request = r
			nextCalled = true
			c.Next()
		})

		wrapped := mw(next)
		wrapped.ServeHTTP(c.Writer, c.Request)

		if !nextCalled {
			c.Abort()
		}
	}
}
