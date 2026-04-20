# Scalable Flash Sale System

A production-grade flash sale service built in Go, deployed on AWS ECS Fargate, designed to handle thousands of concurrent users competing for limited inventory while guaranteeing correctness (no overselling).

Built for CS6650 — Building Scalable Distributed Systems, Northeastern University, Spring 2026.

**Team:** Dawei Feng · Rajkesh Prakash Shetty

---

## The Problem

Imagine Black Friday: 5,000 users click "Buy" simultaneously for 100 items. How do you guarantee exactly 100 get sold — no more, no less — while keeping the system fast and responsive? That's the flash sale problem. This project builds the system from scratch and runs controlled experiments to quantify the tradeoffs.

---

## Architecture

```
Users → ALB → ECS Fargate (Go/Gin) → ElastiCache Redis
                     ↓
              POST /purchase   →  stock.Backend.Purchase()  →  success / sold out
              GET  /health     →  stock.Backend.Remaining() →  healthy / unhealthy
              POST /reset      →  stock.Backend.Reset()     →  reseeds stock
              POST /slow-purchase → simulates slow request  →  for resilience testing
```

### Stock Backends

| Mode | Env Var | Correct at Scale? | Used In |
|------|---------|-------------------|---------|
| In-Memory | `BACKEND_MODE=memory` | ❌ No (isolated per instance) | Experiment 2 baseline |
| Redis DECR | `BACKEND_MODE=redis` + `STOCK_MODE=decr` | ✅ Yes | Experiments 1, 2, 3 |
| Redis Lua | `BACKEND_MODE=redis` + `STOCK_MODE=lua` | ✅ Yes (no race window) | Experiment 4 |

### Resilience Modes

| Mode | Env Var | What it does |
|------|---------|--------------|
| None | `RESILIENCE_MODE=none` | No protection — baseline |
| Fail Fast | `RESILIENCE_MODE=failfast` | Rejects requests exceeding 500ms |
| Bulkhead | `RESILIENCE_MODE=bulkhead` | Caps concurrent requests at 100 |
| All | `RESILIENCE_MODE=all` | Fail Fast + Bulkhead + Circuit Breaker |

---

## Project Structure

```
project/
├── src/                          # Go service
│   ├── cmd/server/main.go        # Entry point — wires config, backends, routes
│   ├── config/config.go          # Env var loader
│   ├── internal/
│   │   ├── stock/backend.go      # Backend interface + memory, DECR, Lua implementations
│   │   ├── handlers/handlers.go  # HTTP handlers (purchase, health, reset, slow-purchase)
│   │   ├── middleware/           # Resilience patterns
│   │   │   ├── failfast.go       # Timeout-based rejection
│   │   │   ├── bulkhead.go       # Concurrency limiter
│   │   │   ├── circuitbreaker.go # State machine (closed/open/half-open)
│   │   │   └── middleware.go     # Wires all three together
│   │   └── queue/sqs.go          # SQS async handler (Exp 5 — future scope)
│   ├── Dockerfile                # Multi-stage Alpine build
│   └── docker-compose.yml        # Local dev with Redis
├── terraform/                    # AWS infrastructure (IaC)
│   ├── main.tf                   # Wires all modules + Docker build/push
│   ├── variables.tf              # Experiment parameters
│   ├── outputs.tf                # ALB DNS, Redis endpoint, ECR URL
│   └── modules/
│       ├── network/              # VPC, subnets, ElastiCache Redis
│       ├── ecr/                  # Container registry
│       ├── alb/                  # Load balancer + target group
│       ├── ecs/                  # Fargate cluster, task definition, service
│       └── logging/              # CloudWatch log group
└── locust/                       # Load test scripts and results
    ├── locustfile.py             # Locust user definitions
    ├── exp3_*.csv / *.html       # Experiment 3 results
    └── exp4_*.csv / *.html       # Experiment 4 results
```

---

## Experiments

### Experiment 1 — Horizontal Scaling (Dawei)
**Question:** Does adding more instances improve throughput while maintaining correctness?

- 1,000 concurrent users, Redis backend, 1/2/4 instances, 60 seconds
- Result: Throughput flat at ~1,515–1,533 RPS due to Locust client CPU bottleneck
- Correctness: PASS on all configurations — Redis DECR held at exactly 100 items sold

### Experiment 2 — In-Memory vs Redis (Dawei)
**Question:** What breaks when you don't use shared state?

- 4 instances, 1,000 users, in-memory vs Redis backends
- In-memory result: 400 items sold (4x oversell — each instance sold its own 100)
- Redis result: 100 items sold (correct)
- Key insight: In-memory is 5% faster but catastrophically wrong at scale

### Experiment 3 — Resilience Patterns (Rajkesh)
**Question:** Do Fail Fast, Bulkhead, and Circuit Breaker improve behavior under load?

- 200 users, 100 stock, 60 seconds, 4 configurations: none / failfast / bulkhead / all
- Best result: "all" mode — 312 req/s (+73%), avg 293ms (-33%), P99 13,000ms (-41%)
- Supplementary 5,000-user test: 638 vs 576 req/s with all patterns enabled

### Experiment 4 — Redis DECR vs Lua Script (Rajkesh)
**Question:** Does a single atomic Lua script eliminate the DECR race window with acceptable overhead?

- 200 users, 100 stock, 60 seconds, DECR vs Lua backends
- Lua overhead: ~8ms avg latency, ~9 req/s throughput reduction
- Lua eliminates race window entirely — single atomic Redis operation

### Experiment 5 — SQS Async Decoupling (Future Scope)
Implemented in `internal/queue/sqs.go` but not yet load tested. When `SQS_QUEUE_URL` is set, the `/purchase` endpoint returns 202 Accepted immediately and a background worker processes stock deductions asynchronously — similar to the async pattern from HW7.

---

## Local Development

### Run with Docker Compose

```bash
cd src
docker compose up --build

# In-memory service → localhost:8080
# Redis service     → localhost:8081
```

### Test the purchase endpoint

```bash
# Health check
curl http://localhost:8081/health

# Buy an item
curl -X POST http://localhost:8081/purchase

# Reset stock
curl -X POST http://localhost:8081/reset

# Test sold out (110 requests against 100 stock)
curl -X POST http://localhost:8081/reset
for i in $(seq 1 110); do
  curl -s -X POST http://localhost:8081/purchase | grep -o '"success":[a-z]*'
done | sort | uniq -c
# Expected: 100 "success":true, 10 "success":false
```

### Test resilience patterns locally

```bash
# Start with all patterns enabled
RESILIENCE_MODE=all docker compose up

# Test Fail Fast (500ms timeout)
curl -X POST http://localhost:8081/slow-purchase
# Returns: {"message":"request timeout — fail fast","success":false}

# Test Circuit Breaker (fire 10 slow requests to trip it)
for i in $(seq 1 10); do curl -s -X POST http://localhost:8081/slow-purchase & done
# After 5+ failures: {"message":"circuit open — service unavailable"}

# Wait for recovery (5s cooldown)
sleep 6 && curl -X POST http://localhost:8081/purchase
```

---

## AWS Deployment

### Prerequisites
- AWS Learner Lab credentials configured (`aws configure`)
- Docker running
- Terraform >= 1.5.0

### Deploy

```bash
cd terraform
terraform init
terraform apply -var="aws_region=us-east-1"
```

### Switch experiment configurations

```bash
# Experiment 1 & 2 — scale instances
terraform apply -var="ecs_desired_count=4"

# Experiment 2 — in-memory backend
terraform apply -var="backend_mode=memory"

# Experiment 3 — resilience modes
terraform apply -var="resilience_mode=failfast"
terraform apply -var="resilience_mode=bulkhead"
terraform apply -var="resilience_mode=all"

# Experiment 4 — Lua script
terraform apply -var="stock_mode=lua"
```

### Tear down

```bash
terraform destroy -auto-approve
```

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP port |
| `STOCK_COUNT` | `100` | Initial stock |
| `BACKEND_MODE` | `memory` | `memory` or `redis` |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `RESILIENCE_MODE` | `none` | `none`, `failfast`, `bulkhead`, `all` |
| `STOCK_MODE` | `decr` | `decr` or `lua` |
| `SQS_QUEUE_URL` | `""` | SQS queue URL (enables async mode) |

---

## Key Results Summary

| Experiment | Key Finding |
|------------|-------------|
| Exp 1 — Horizontal Scaling | Correctness holds at all scales; client CPU was the bottleneck |
| Exp 2 — Memory vs Redis | In-memory oversells 4x; Redis is a correctness requirement |
| Exp 3 — Resilience Patterns | All combined: +73% throughput, -41% P99 vs no protection |
| Exp 4 — DECR vs Lua | Lua eliminates race window at -3.5% throughput cost |