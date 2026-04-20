package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// FailFast rejects any request that takes longer than the given timeout.
// Under extreme load, slow requests are immediately cut off rather than
// piling up and blocking resources for everyone else.
func FailFast(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)

		done := make(chan struct{})

		go func() {
			c.Next()
			close(done)
		}()

		select {
		case <-done:
			// Request completed within timeout — all good
			return
		case <-ctx.Done():
			// Timeout exceeded — reject immediately
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "request timeout — fail fast",
			})
		}
	}
}