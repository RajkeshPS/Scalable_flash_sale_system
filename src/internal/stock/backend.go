package stock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/redis/go-redis/v9"
)

var ErrSoldOut = errors.New("sold out")

type Backend interface {
	Purchase(ctx context.Context) error
	Remaining(ctx context.Context) (int64, error)
}

// ── In-Memory Backend ──────────────────────────────────────────
// Correct for a single instance. Breaks under horizontal scaling
// because each instance has its own counter (Experiment 2 baseline).

type MemoryBackend struct {
	stock int64
}

func NewMemoryBackend(initial int) *MemoryBackend {
	return &MemoryBackend{stock: int64(initial)}
}

func (m *MemoryBackend) Purchase(_ context.Context) error {
	for {
		current := atomic.LoadInt64(&m.stock)
		if current <= 0 {
			return ErrSoldOut
		}
		if atomic.CompareAndSwapInt64(&m.stock, current, current-1) {
			return nil
		}
	}
}

func (m *MemoryBackend) Remaining(_ context.Context) (int64, error) {
	return atomic.LoadInt64(&m.stock), nil
}

// ── Redis Backend ──────────────────────────────────────────────
// Supports two stock modes:
// "decr" — atomic DECR + INCR on negative (Exp 2, 3)
// "lua"  — single atomic Lua script, no race window (Exp 4)

const redisStockKey = "flash_sale:stock"

// Lua script: atomically check and decrement in one operation.
// Returns 1 on success, 0 if sold out.
// This eliminates the race window between DECR going negative
// and INCR correcting it in the "decr" mode.
var luaScript = redis.NewScript(`
local stock = tonumber(redis.call('GET', KEYS[1]))
if stock == nil or stock <= 0 then
    return 0
end
redis.call('DECR', KEYS[1])
return 1
`)

type RedisBackend struct {
	client    *redis.Client
	mu        sync.Once
	init      int64
	stockMode string // "decr" or "lua"
}

func NewRedisBackend(addr string, initialStock int, stockMode string) *RedisBackend {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &RedisBackend{
		client:    client,
		init:      int64(initialStock),
		stockMode: stockMode,
	}
}

func (r *RedisBackend) Init(ctx context.Context) error {
	return r.client.SetNX(ctx, redisStockKey, r.init, 0).Err()
}

func (r *RedisBackend) Purchase(ctx context.Context) error {
	switch r.stockMode {
	case "lua":
		return r.purchaseLua(ctx)
	default:
		return r.purchaseDecr(ctx)
	}
}

// purchaseDecr — Exp 2 & 3 approach
func (r *RedisBackend) purchaseDecr(ctx context.Context) error {
	remaining, err := r.client.Decr(ctx, redisStockKey).Result()
	if err != nil {
		return err
	}
	if remaining < 0 {
		r.client.Incr(ctx, redisStockKey)
		return ErrSoldOut
	}
	return nil
}

// purchaseLua — Exp 4 approach, fully atomic
func (r *RedisBackend) purchaseLua(ctx context.Context) error {
	result, err := luaScript.Run(ctx, r.client, []string{redisStockKey}).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrSoldOut
	}
	return nil
}

func (r *RedisBackend) Remaining(ctx context.Context) (int64, error) {
	val, err := r.client.Get(ctx, redisStockKey).Int64()
	if err != nil {
		return 0, err
	}
	return val, nil
}

func (r *RedisBackend) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisBackend) Reset(ctx context.Context) error {
	return r.client.Set(ctx, redisStockKey, r.init, 0).Err()
}