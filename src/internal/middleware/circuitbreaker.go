package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type State int

const (
	StateClosed   State = iota // Normal — requests flow through
	StateOpen                  // Tripped — all requests rejected
	StateHalfOpen              // Testing — one request allowed through
)

type CircuitBreaker struct {
	mu             sync.Mutex
	state          State
	failures       int
	failureThresh  int
	successThresh  int
	successes      int
	cooldown       time.Duration
	lastTripped    time.Time
}

func NewCircuitBreaker(failureThresh int, successThresh int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:         StateClosed,
		failureThresh: failureThresh,
		successThresh: successThresh,
		cooldown:      cooldown,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if cooldown has passed — move to half-open
		if time.Since(cb.lastTripped) > cb.cooldown {
			cb.state = StateHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case StateHalfOpen:
		// Only allow one probe request through
		return cb.successes == 0
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.successThresh {
			// Recovered — close the circuit
			cb.state = StateClosed
			cb.failures = 0
		}
	} else {
		cb.failures = 0
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	if cb.failures >= cb.failureThresh {
		cb.state = StateOpen
		cb.lastTripped = time.Now()
	}
}

func (cb *CircuitBreaker) StateString() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// CircuitBreakerMiddleware wraps the circuit breaker logic as Gin middleware.
func CircuitBreakerMiddleware(cb *CircuitBreaker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cb.Allow() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "circuit open — service unavailable",
				"state":   cb.StateString(),
			})
			return
		}

		// Use a channel to detect if downstream aborted
		c.Next()

		status := c.Writer.Status()
		if status == 0 || status >= 500 || status == 503 || c.IsAborted() {
			cb.RecordFailure()
		} else {
			cb.RecordSuccess()
		}
	}
}