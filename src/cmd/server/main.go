package main

import (
	"context"
	"log"

	"flash-sale/config"
	"flash-sale/internal/handlers"
	"flash-sale/internal/middleware"
	"flash-sale/internal/queue"
	"flash-sale/internal/stock"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	// ── Stock Backend ──────────────────────────────────────────
	var backend stock.Backend

	switch cfg.BackendMode {
	case "redis":
		log.Printf("Backend: Redis (%s mode)", cfg.StockMode)
		rb := stock.NewRedisBackend(cfg.RedisAddr, cfg.StockCount, cfg.StockMode)
		if err := rb.Init(ctx); err != nil {
			log.Fatalf("Failed to initialize Redis stock: %v", err)
		}
		if err := rb.Ping(ctx); err != nil {
			log.Fatalf("Cannot reach Redis at %s: %v", cfg.RedisAddr, err)
		}
		log.Printf("Redis connected at %s, stock initialized to %d", cfg.RedisAddr, cfg.StockCount)
		backend = rb

	default:
		log.Println("Backend: In-Memory (isolated per instance — breaks at scale)")
		backend = stock.NewMemoryBackend(cfg.StockCount)
		log.Printf("In-memory stock initialized to %d", cfg.StockCount)
	}

	// ── SQS Setup (Exp 5) ──────────────────────────────────────
	var h *handlers.Handler

	if cfg.SQSQueueURL != "" {
		log.Printf("Exp 5: SQS async mode enabled, queue: %s", cfg.SQSQueueURL)
		if err := queue.InitSQS(ctx); err != nil {
			log.Fatalf("Failed to initialize SQS client: %v", err)
		}
		// Start background worker to process purchase messages
		go queue.StartWorker(ctx, cfg.SQSQueueURL, backend)
		h = handlers.NewWithSQS(backend, cfg.SQSQueueURL)
	} else {
		h = handlers.New(backend)
	}

	// ── Resilience Middleware (Exp 3) ──────────────────────────
	log.Printf("Resilience mode: %s", cfg.ResilienceMode)
	middlewares := middleware.Build(cfg.ResilienceMode, middleware.DefaultConfig())

	// ── Routes ─────────────────────────────────────────────────
	r := gin.Default()

	// Health and reset bypass resilience middleware
	r.GET("/health", h.Health)
	r.POST("/reset", h.Reset)

	// Purchase route gets the full middleware chain
	purchase := r.Group("/")
	for _, m := range middlewares {
		purchase.Use(m)
	}
	purchase.POST("/purchase", h.Purchase)
	purchase.POST("/slow-purchase", h.SlowPurchase)

	log.Printf("Starting flash sale service on port %s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}