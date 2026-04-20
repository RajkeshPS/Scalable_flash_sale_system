package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Bulkhead limits the number of requests processed concurrently.
// If the limit is reached, new requests are immediately rejected
// rather than queuing up and exhausting memory/connections.
// This isolates the flash sale endpoint from being overwhelmed.
func Bulkhead(maxConcurrent int) gin.HandlerFunc {
	sem := make(chan struct{}, maxConcurrent)

	return func(c *gin.Context) {
		select {
		case sem <- struct{}{}:
			// Slot acquired — process the request
			defer func() { <-sem }()
			c.Next()
		default:
			// All slots full — reject immediately
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": "server at capacity — bulkhead rejected",
			})
		}
	}
}
