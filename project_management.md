# Project Management — Scalable Flash Sale System

**Team:** Dawei Feng, Rajkesh Prakash Shetty  
**Course:** CS6650 — Building Scalable Distributed Systems  
**Duration:** 6 weeks (March–April 2026)

---

## 1. Initial design and task breakdown

We started with a clear question: how do you serve thousands of concurrent users competing for limited stock without overselling? We broke the project into three layers and divided ownership:

| Area | Owner | Description |
|------|-------|-------------|
| Go service + stock backends | Rajkesh | Core purchase logic, in-memory (atomic CAS) and Redis (atomic DECR + Lua script) backends, /purchase, /health, /reset endpoints |
| Infrastructure (Terraform) | Dawei | VPC, ALB, ECS Fargate, ElastiCache Redis, ECR, CloudWatch — all as IaC with parameterized variables for experiment control |
| Load testing + experiments 1 & 2 | Dawei | Locust scripts, automated experiment runner, result visualization, report generation |
| Resilience patterns (Exp 3) | Rajkesh | Fail Fast, Bulkhead, Circuit Breaker implementation and 200-user load testing (100 as stock) |
| Lua script optimization (Exp 4) | Rajkesh | Redis Lua atomic script replacing DECR/INCR two-step, correctness and performance comparison |
| SQS async decoupling (Exp 5) | Rajkesh | Implemented but deferred to future scope (see Section 5) |

---

## 2. How the project progressed

### Week 1–2: Foundation

- Set up Go project structure: `cmd/server/main.go`, `config/`, `internal/stock/`, `internal/handlers/`
- Implemented both stock backends with a shared `Backend` interface for easy switching
- Wrote Terraform modules: network (VPC + subnets + ElastiCache), ECR, ALB, ECS, logging
- Created Dockerfile (multi-stage build) and docker-compose.yml for local dev
- **Problem encountered:** Terraform's Docker provider failed to connect on Windows (Docker Desktop not running as elevated). Resolved by ensuring Docker Desktop was started before running Terraform.

### Week 3: Experiment infrastructure

- Built `locustfile.py` with custom correctness tracking (success/sold-out/error counters with gevent locks)
- Built `run_experiments.sh` to automate the full cycle: Terraform apply → health check → stock reset → Locust → verify
- **Problem encountered:** ElastiCache is VPC-internal, so `redis-cli` from local machine couldn't reach it. Solved by adding a `POST /reset` endpoint to the Go service and resetting stock via ALB instead.
- **Problem encountered:** Terraform built a new Docker image but didn't push it to ECR (kreuzwerker/docker provider issue). ECS kept running the old image. Resolved with `terraform taint` + manual `docker push` + `aws ecs update-service --force-new-deployment`.

### Week 4: Experiments 1 & 2

- Ran Experiment 1: Redis mode with 1, 2, 4 instances — all passed correctness (exactly 100 sold)
- Ran Experiment 2: Memory vs Redis with 4 instances — memory oversold by 300 (400 total), Redis correct
- **Problem encountered:** Locust hit 90%+ CPU on the local machine, capping throughput at ~1,500 RPS regardless of instance count. This means Experiment 1 couldn't show true server-side scaling gains. Documented as a limitation — would need distributed Locust for higher load.
- Built `visualize_results.py` to generate 10 charts from CSV data
- Built `generate_report.js` to produce a formatted Word document with all figures

### Week 5: Experiments 3 & 4 

- Implemented resilience middleware package: Fail Fast (configurable timeout), Bulkhead (semaphore-based concurrency limiter), Circuit Breaker (state machine with closed/open/half-open states)
- Added `STOCK_MODE` env var supporting both DECR and Lua script backends (Exp 4)
- Implemented Lua script backend — single atomic Redis operation eliminates the race window present in the DECR/INCR two-step approach
- Added `RESILIENCE_MODE` env var (`none`, `failfast`, `bulkhead`, `all`) for zero-code experiment switching
- Implemented SQS async purchase handler and background worker (Exp 5 — implemented but deferred to future work due to time constraints)
- Deployed and tested all patterns locally with Docker Compose — verified Bulkhead rejections, Fail Fast timeouts, Circuit Breaker trips and recovery
- Deployed to AWS via Terraform and ran Locust for all 4 resilience configurations (none, failfast, bulkhead, all) with 200 concurrent users against 100 stock items
- Also ran supplementary 5,000 user tests confirming resilience patterns improve throughput (576 → 638 req/s) even at extreme scale
- **Problem encountered:** ECR push failed intermittently from GCP Cloud Shell due to network restrictions. Resolved by retrying the push command.
- **Problem encountered:** Circuit Breaker was not tripping because Fail Fast aborted requests before the CB middleware could record failures. Resolved by reordering middleware chain (Bulkhead → CB → Fail Fast) and detecting aborted requests via `c.IsAborted()`.
- **Problem encountered:** ECS tasks running stale image after task definition update — new env vars not picked up immediately. Resolved by waiting for rolling deployment to complete before running Locust.

### Week 6: Write-up, video, submission

- Final report combining Dawei's Exp 1 & 2 results with Rajkesh's Exp 3 & 4 results
- Updated project management documentation
- Recorded individual video walkthroughs
- Piazza community post with links to 3 similar projects
---

## 3. Problems encountered and how we resolved them

| Problem                                        | Impact                                                             | Resolution                                                                  |
| ---------------------------------------------- | ------------------------------------------------------------------ | --------------------------------------------------------------------------- |
| Docker provider can't connect on Windows       | Terraform apply fails at image build                               | Start Docker Desktop before running scripts                                 |
| Terraform builds image but doesn't push to ECR | ECS runs stale container without new endpoints                     | `terraform taint docker_image.app` + `docker push` + `force-new-deployment` |
| ElastiCache not reachable from local machine   | Can't reset stock between experiments                              | Added `/reset` HTTP endpoint, reset via ALB                                 |
| `locust` command not found in Git Bash         | Automated script fails at test step                                | Changed script to use `python -m locust`                                    |
| Locust stats_history.csv column name mismatch  | "Throughput Over Time" charts rendered empty                       | Fixed Python script to match actual CSV column names                        |
| Client CPU bottleneck (90%+) during load test  | Throughput capped at ~1,500 RPS, can't measure true server scaling | Documented as limitation; recommend distributed Locust for future work      |
| ECR push fails intermittently from GCP Cloud Shell | Cannot push new Docker image to AWS | Retry push command — intermittent network restriction on GCP egress |
| Circuit Breaker not tripping under Fail Fast | CB never recorded failures, patterns not cooperating | Reordered middleware: Bulkhead → CB → Fail Fast, used `c.IsAborted()` to detect rejections |
| ECS running stale image after task definition update | New env vars not applied to running tasks | Wait for rolling deployment to complete before running load tests |

---

## 4. Tools and workflow

- **Version control:** Git + GitHub (commits, branches, issues for tracking)
- **Infrastructure:** Terraform (parameterized — `ecs_desired_count`, `backend_mode`, `resilience_mode`, `stock_mode` switch experiments with one variable)
- **CI/CD:** Docker multi-stage build → ECR → ECS rolling deployment
- **Testing:** Locust (Python) with automated bash orchestration
- **Analysis:** Python (pandas + matplotlib) for charts, Node.js (docx-js) for report

---

## 5. Future Scope

**Experiment 5 — SQS Async Decoupling (implemented, not yet tested)**
The codebase includes a full SQS-based async purchase handler and background worker. When `SQS_QUEUE_URL` is set, the `/purchase` endpoint immediately returns 202 Accepted and enqueues the purchase to SQS. A goroutine worker polls the queue and processes stock deductions asynchronously. This pattern, similar to what was validated in HW7, is expected to dramatically improve throughput under extreme load by decoupling request intake from stock processing. Full load testing against a live SQS queue is deferred to future work.

**Other future directions:**
- Distributed Locust across multiple machines to remove the client CPU bottleneck (~1,500 RPS ceiling observed in Experiments 1 & 2)
- Auto-scaling ECS tasks based on CloudWatch metrics instead of manual Terraform variable changes
- AWS X-Ray distributed tracing to follow requests across ALB, ECS instances, and Redis
- CloudWatch alarms on P99 latency and error rate thresholds for production observability
- Replace ElastiCache single-node Redis with a cluster for higher availability and throughput