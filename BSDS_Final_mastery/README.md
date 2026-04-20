# CS 6650 Final Mastery — Album Store API

## Rajkesh Prakash Shetty

ChaosArena Score: **170/190** (Correctness: 110/110, Load: 60/80)

---

## What Was Asked

Build and deploy a REST API for a photo album service from scratch. Submit the live URL to ChaosArena which automatically tests correctness (110 pts) and load performance (80 pts). Total possible: 190 points.

The API has 6 endpoints:
- `GET /health` — returns `{"status":"ok"}`
- `PUT /albums/:album_id` — idempotent create/update album
- `GET /albums/:album_id` — fetch one album
- `GET /albums` — list all albums
- `POST /albums/:album_id/photos` — accept photo upload, return 202 immediately, process in background
- `GET /albums/:album_id/photos/:photo_id` — check processing status, returns URL when completed
- `DELETE /albums/:album_id/photos/:photo_id` — delete DB record and S3 file within 5 seconds

The photo upload is async. The handler must assign a per-album sequence number (seq) atomically, return 202 with the seq, and process the upload in the background. ChaosArena polls the status endpoint until it sees "completed" with a working URL.

---

## Architecture

```
                    ┌─────────────┐
    Internet ──────►│     ALB     │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Fargate  │ │ Fargate  │ │ Fargate  │
        │ Task 1   │ │ Task 2   │ │ Task 3   │
        │ Go + 30  │ │ Go + 30  │ │ Go + 30  │
        │ workers  │ │ workers  │ │ workers  │
        └────┬─────┘ └────┬─────┘ └────┬─────┘
             │             │             │
             ▼             ▼             ▼
        ┌──────────┐  ┌──────────────────────┐
        │   RDS    │  │         S3           │
        │ Postgres │  │  Transfer Accel +    │
        │ t3.small │  │  Public Read         │
        └──────────┘  └──────────────────────┘
```

**Traffic Layer:** ALB is the single public entry point. Distributes requests across 3 Fargate tasks. Health checks /health every 15 seconds.

**Compute Layer:** 3 ECS Fargate tasks, each 1 vCPU and 3GB RAM. Each runs the same Go binary with Gin for routing. 30 background goroutines per task read photo jobs from a buffered Go channel and upload to S3.

**Data Layer:** RDS PostgreSQL db.t3.small (~150 connections) stores album and photo metadata. S3 with Transfer Acceleration and public read bucket policy stores the actual photo files.

### Photo Upload Flow

1. POST handler reads multipart file
2. Runs `UPDATE albums SET photo_count = photo_count + 1 RETURNING photo_count` for atomic seq
3. Inserts photo row with status "processing"
4. Pushes job onto Go channel
5. Returns 202 with photo_id, seq, status "processing"
6. Worker goroutine picks up job, uploads to S3 via multipart uploader
7. Worker updates photo row to status "completed" with public S3 URL

### Delete Flow

1. Look up S3 key from database
2. Delete database row
3. Delete S3 object (synchronous, not background)
4. Return 204

Delete must be synchronous because ChaosArena checks the S3 URL immediately after.

---

## Tech Stack

- **Language:** Go with Gin framework
- **Database:** PostgreSQL on RDS (db.t3.small)
- **Storage:** S3 with Transfer Acceleration, public read bucket policy
- **Compute:** ECS Fargate (3 tasks, 1 vCPU, 3GB RAM each)
- **Load Balancer:** Application Load Balancer
- **Infrastructure as Code:** Terraform
- **Container:** Docker multi-stage build (golang:1.21-alpine builder, alpine:3.19 runtime)
- **AWS SDK:** AWS SDK v1 for Go (v2 had checksum compatibility issues)
- **CI/CD:** deploy.sh script (ECR login, Docker build, push, ECS force deploy)

---

## Database Schema

**albums table:**
- album_id (TEXT, primary key)
- title (TEXT)
- description (TEXT)
- owner (TEXT)
- photo_count (INT) — doubles as atomic seq counter

**photos table:**
- photo_id (TEXT, primary key)
- album_id (TEXT, foreign key to albums)
- seq (INT)
- status (TEXT) — "processing", "completed", or "failed"
- url (TEXT) — public S3 URL, populated when completed
- s3_key (TEXT) — for deletion

**Indexes:**
- idx_photos_album_id on photos(album_id)
- idx_photos_status on photos(status)

---

## Project Structure

```
album-store/
├── main.go          # All handlers, workers, DB, S3 logic
├── go.mod           # Dependencies
├── go.sum           # Dependency checksums
├── Dockerfile       # Multi-stage build
├── deploy.sh        # Build, push to ECR, update ECS
└── terraform/
    └── main.tf      # All AWS infrastructure
```

---

## Journey From 20 to 170 Points

### Submission 1 — Score: 20
S1 and S2 passed. S3 failed because AWS SDK v2 sends trailing checksum headers that S3 returned 501 NotImplemented for. Switched to SDK v1.

### Submission 2 — Score: 20
Same error. Old ECS tasks hadn't rolled out yet. Had to wait for deployment.

### Submission 3 — Score: 134
All critical scenarios S1 through S5 passed. S10 (per-album seq) failed because a separate EXISTS check raced with concurrent uploads. S15 got 0 due to 100% error rate on large payloads.

### Key Fixes After 134
- Removed separate EXISTS check. The UPDATE RETURNING naturally returns ErrNoRows if album doesn't exist.
- Added MaxMultipartMemory = 256MB for large uploads
- Bumped worker count from 10 to 20

### Connection Exhaustion — Score: 127
S12 dropped to 0 with 37% error rate. CloudWatch logs showed hundreds of "remaining connection slots are reserved" errors. db.t3.micro only has ~80 connections, we had 2 tasks with MaxOpenConns=100 each.

### The Big Fix — Score: 169
Upgraded RDS to db.t3.small (150 connections). Bumped Fargate to 1 vCPU/3GB RAM. Set ALB idle timeout to 120s. Added HTTP server timeouts. Tuned MaxOpenConns=60 per task. S10 passed, S11 and S13 hit perfect scores.

### S3 Delete Bug — Score: 40
Tried async S3 delete in a goroutine. ChaosArena checked the URL immediately and file was still there. Reverted to synchronous delete.

### Transfer Acceleration + Multipart — Score: 170
Enabled S3 Transfer Acceleration. Switched to s3manager.Uploader with 5MB parts and 10 concurrent threads. Added 3rd ECS task. S14 hit perfect 15/15. S15 improved from 0 to 9 points.

### Streaming Upload Attempt — Score: 157
Tried io.Pipe to stream file directly to S3 without buffering. Broke S10 due to race condition with request body. Reverted.

---

## Where Points Were Lost

**S12 — Concurrent Photo Uploads: 6/15 (lost 9 pts)**
p95 was 5298ms. Bottleneck is S3 upload throughput under heavy concurrency. Many workers competing for network bandwidth.

**S15 — Large Payload Upload: 9/20 (lost 11 pts)**
Accept p95 was 10229ms because handler reads entire file into memory before returning 202. Complete p95 was 12339ms due to S3 upload time for large files.

### Possible Improvements
- Switch from Fargate to EC2 for higher network bandwidth (up to 10 Gbps)
- Stream uploads with io.Pipe so handler doesn't buffer the full file
- Increase Fargate task count to spread upload load
- Upload to S3 in the handler directly instead of passing through worker channel

---

## Key Configuration

| Setting | Value | Why |
|---------|-------|-----|
| MaxOpenConns | 60 per task | 3 tasks x 60 = 180, under RDS 150 limit |
| MaxIdleConns | 30 per task | Keep warm connections ready |
| MaxMultipartMemory | 512MB | Support uploads up to 200MB |
| HTTP Timeouts | 120s read/write/idle | Large uploads need time |
| ALB Idle Timeout | 120s | Match server timeouts |
| Worker Count | 30 per task | Parallel S3 uploads |
| Job Channel Buffer | 5000 | Handle burst traffic |
| S3 Part Size | 5MB | Multipart upload chunks |
| S3 Upload Concurrency | 10 | Parallel chunk uploads |
| Deregistration Delay | 30s | Faster deployments |

---

## Key Bugs and Fixes

| Bug | Symptom | Root Cause | Fix |
|-----|---------|------------|-----|
| SDK v2 checksum | S3 returns 501 | SDK v2 sends trailing checksum headers | Switch to SDK v1 |
| SSL connection refused | Tasks crash on startup | sslmode=disable in connection string | Change to sslmode=require |
| Connection exhaustion | 37% error rate, tasks returning 500 | db.t3.micro only has 80 connections | Upgrade to db.t3.small |
| S3 delete race | ChaosArena sees file still exists after delete | Async delete in goroutine returns before S3 processes | Make delete synchronous |
| S10 seq failure | 404 on photo upload | Separate EXISTS check races with concurrent uploads | Remove EXISTS check, use UPDATE RETURNING |

---

## How to Deploy

```bash
# 1. Configure AWS credentials
aws configure

# 2. Create infrastructure
cd terraform
terraform init
terraform apply

# 3. Build and deploy
cd ..
./deploy.sh

# 4. Test
curl http://<alb-url>/health

# 5. Submit to ChaosArena
curl -X POST http://<chaosarena-url>/submit \
  -H "Content-Type: application/json" \
  -d '{"email":"you@northeastern.edu","nickname":"yourname","base_url":"http://<alb-url>","contract":"v1-album-store"}'

# 6. Destroy when done
cd terraform
terraform destroy
```

---

## Course Concepts Applied

- **Stateless services with shared external storage** (HW2 UhOh moment) — Fargate tasks are stateless, all state in RDS and S3
- **Horizontal scaling behind a load balancer** (HW6) — 3 tasks behind ALB
- **Async processing** (HW7) — 202 response with background workers
- **Atomic operations for concurrency** — UPDATE RETURNING for seq assignment
- **Connection pool management** — tuning MaxOpenConns to match RDS limits
- **Infrastructure as Code** (HW2+) — Terraform for all resources
- **Information hiding** (Parnas paper) — workers don't know about HTTP, handlers don't know about S3
