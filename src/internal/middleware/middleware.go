package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
)

// Config holds tunable parameters for all resilience patterns.
// These are set once at startup based on RESILIENCE_MODE env var.
type Config struct {
	// Fail Fast
	Timeout time.Duration

	// Bulkhead
	MaxConcurrent int

	// Circuit Breaker
	FailureThresh int
	SuccessThresh int
	Cooldown      time.Duration
}

// DefaultConfig returns sensible defaults for Experiment 3.
func DefaultConfig() Config {
	return Config{
		Timeout:       500 * time.Millisecond,
		MaxConcurrent: 100,
		FailureThresh: 10,
		SuccessThresh: 3,
		Cooldown:      5 * time.Second,
	}
}

// Build returns the middleware chain for the given mode.
// mode options: "none", "failfast", "bulkhead", "all"
func Build(mode string, cfg Config) []gin.HandlerFunc {
	cb := NewCircuitBreaker(cfg.FailureThresh, cfg.SuccessThresh, cfg.Cooldown)

	switch mode {
	case "failfast":
		return []gin.HandlerFunc{
			FailFast(cfg.Timeout),
		}
	case "bulkhead":
		return []gin.HandlerFunc{
			Bulkhead(cfg.MaxConcurrent),
		}
	case "all":
		return []gin.HandlerFunc{
			Bulkhead(cfg.MaxConcurrent),
			CircuitBreakerMiddleware(cb),
			FailFast(cfg.Timeout),
		}
	default:
		// "none" — no protection, baseline for Experiment 3
		return []gin.HandlerFunc{}
	}
}
