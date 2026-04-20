package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port           string
	StockCount     int
	BackendMode    string // "memory" or "redis"
	RedisAddr      string
	ResilienceMode string // "none", "failfast", "bulkhead", "all"
	StockMode      string // "decr" or "lua" (Exp 4)
	SQSQueueURL    string // SQS queue URL (Exp 5)
}

func Load() *Config {
	stockCount, err := strconv.Atoi(getEnv("STOCK_COUNT", "100"))
	if err != nil {
		stockCount = 100
	}

	return &Config{
		Port:           getEnv("PORT", "8080"),
		StockCount:     stockCount,
		BackendMode:    getEnv("BACKEND_MODE", "memory"),
		RedisAddr:      getEnv("REDIS_ADDR", "localhost:6379"),
		ResilienceMode: getEnv("RESILIENCE_MODE", "none"),
		StockMode:      getEnv("STOCK_MODE", "decr"),
		SQSQueueURL:    getEnv("SQS_QUEUE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}