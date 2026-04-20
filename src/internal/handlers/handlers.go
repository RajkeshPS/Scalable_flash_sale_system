package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"flash-sale/internal/queue"
	"flash-sale/internal/stock"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	backend  stock.Backend
	sqsQueue string // SQS queue URL, empty if not using async mode
}

func New(backend stock.Backend) *Handler {
	return &Handler{backend: backend}
}

func NewWithSQS(backend stock.Backend, sqsQueueURL string) *Handler {
	return &Handler{backend: backend, sqsQueue: sqsQueueURL}
}

// Purchase handles POST /purchase
// If SQS queue is configured, enqueues the purchase async (Exp 5).
// Otherwise processes synchronously (Exp 1, 2, 3, 4).
func (h *Handler) Purchase(c *gin.Context) {
	if h.sqsQueue != "" {
		h.purchaseAsync(c)
		return
	}
	h.purchaseSync(c)
}

// purchaseSync — used for Exp 1, 2, 3, 4
func (h *Handler) purchaseSync(c *gin.Context) {
	err := h.backend.Purchase(c.Request.Context())
	if err != nil {
		if errors.Is(err, stock.ErrSoldOut) {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"message": "sold out",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "internal error",
		})
		return
	}

	remaining, _ := h.backend.Remaining(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   "purchase successful",
		"remaining": remaining,
	})
}

// purchaseAsync — used for Exp 5
// Immediately returns 202 Accepted and enqueues the purchase to SQS.
// A worker processes the actual stock deduction asynchronously.
func (h *Handler) purchaseAsync(c *gin.Context) {
	err := enqueuePurchase(c.Request.Context(), h.sqsQueue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to enqueue purchase",
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "purchase queued",
	})
}

// Health handles GET /health
func (h *Handler) Health(c *gin.Context) {
	remaining, err := h.backend.Remaining(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"remaining": remaining,
	})
}

// Reset handles POST /reset
func (h *Handler) Reset(c *gin.Context) {
	type resettable interface {
		Reset(ctx context.Context) error
	}

	rb, ok := h.backend.(resettable)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "reset not supported for this backend",
		})
		return
	}

	if err := rb.Reset(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	remaining, _ := h.backend.Remaining(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   "stock reset",
		"remaining": remaining,
	})
}

// enqueuePurchase sends a purchase message to SQS.
func enqueuePurchase(ctx context.Context, queueURL string) error {
	return queue.SendPurchase(ctx, queueURL)
}

// SlowPurchase handles POST /slow-purchase
// Simulates a slow request for testing Fail Fast middleware.
func (h *Handler) SlowPurchase(c *gin.Context) {
	time.Sleep(500 * time.Millisecond) // 500ms > 200ms timeout
	h.purchaseSync(c)
}