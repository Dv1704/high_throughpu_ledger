# 🏦 High-Frequency Ledger & Settlement Engine

> A production-grade, high-throughput financial transaction processing system built in **Go**, implementing and benchmarking all core strategies for building scalable fintech infrastructure.

---

## � Benchmark Results

All benchmarks run on a **4-core** machine with **2 Postgres shards** and **Redis**, using the built-in multi-scenario benchmark suite.

### Performance Summary

| Test | TPS | P50 (ms) | P95 (ms) | P99 (ms) | Max (ms) | Error Rate |
| :--- | ---: | ---: | ---: | ---: | ---: | ---: |
| Baseline (10 workers) | **3,084** | 2.55 | 6.08 | 9.36 | 35.73 | 0.0% |
| Medium Load (50 workers) | **3,088** | 13.30 | 28.15 | 40.50 | 96.80 | 0.0% |
| High Load (200 workers) | **3,783** | 43.84 | 90.82 | 128.40 | 222.45 | 0.0% |
| Burst Test (500 workers) | **3,894** | 113.88 | 200.24 | 255.35 | 355.09 | 0.0% |

### Key Findings

| Observation | Detail |
| :--- | :--- |
| 📈 **Throughput scales under pressure** | +26% TPS from baseline (3,084) to burst (3,894) |
| ⚖️ **Throughput ↔ Latency trade-off** | P99 rises from 9ms → 255ms as concurrency increases |
| 🛡️ **Zero errors under all loads** | System absorbs 500-worker bursts with 0% error rate |
| 💽 **Batching reduces DB IOPS** | 100-tx batch commits keep disk I/O efficient at 3,800+ TPS |
| 🔀 **Sharding distributes load** | Even-odd user IDs routed to separate Postgres instances |

---

## ✅ Strategy Implementation Map

Every strategy from the high-throughput systems document is implemented and quantified:

| # | Strategy (from PDF) | How It's Implemented | Where to Find It | Quantified Result |
|:-:|:---------------------|:---------------------|:------------------|:------------------|
| 1 | **Throughput vs. Latency Trade-off** | Switchable batch size/flush interval; compare P99 across load levels | Benchmark summary table | P99: 9ms (low load) → 255ms (burst) |
| 2 | **Horizontal Scaling & Load Balancing** | Application-level DB sharding across 2 Postgres instances | `internal/db/sharded_client.go` | Transactions distributed by `user_id % shards` |
| 3 | **Concurrency & Parallelism** | Go goroutines + configurable worker pool (`NumCPU * 2` workers) | `internal/queue/worker.go` | 8 workers on 4-core machine, all cores utilized |
| 4 | **Asynchronous Processing with Queues** | Channel-based in-memory queue decouples HTTP accept from DB commit | `internal/queue/worker.go` | API returns 202 instantly; workers process async |
| 5 | **Caching (Read-Aside)** | Redis cache for account balance lookups; cache-miss falls through to DB | `internal/cache/redis.go` | Sub-ms cache hits; 10-min TTL |
| 6 | **Write Batching & I/O Optimization** | Batcher accumulates 100 txs or flushes every 50ms in a single DB transaction | `internal/ledger/batcher.go` | Single commit per batch instead of per-row |
| 7 | **Database Indexing** | Composite indexes on `(from_account_id, created_at)`, single indexes on status, user_id | `init.sql` | Fast lookups for balance checks and audit queries |

---

## 🏗 Architecture

```
                    ┌─────────────────┐
                    │   HTTP Client   │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │   FastHTTP API  │  ← High-perf HTTP server (262K concurrency)
                    │   (main.go)     │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
     ┌────────▼───────┐     │     ┌────────▼────────┐
     │  Redis Cache   │     │     │  Balance Check  │
     │  (read-aside)  │     │     │  (cache → DB)   │
     └────────────────┘     │     └─────────────────┘
                             │
                    ┌────────▼────────┐
                    │  Worker Pool    │  ← Async goroutine workers
                    │  (channel queue)│
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │  Write Batcher  │  ← 100 txs / 50ms flush
                    └───┬─────────┬───┘
                        │         │
              ┌─────────▼──┐  ┌──▼─────────┐
              │  Shard 1   │  │  Shard 2   │  ← Postgres shards
              │ (even IDs) │  │ (odd IDs)  │
              └────────────┘  └────────────┘
```

---

## 🛠 Project Structure

```
high_throughput_system/
├── main.go                          # FastHTTP server, routing, account seeding, live stats
├── internal/
│   ├── db/
│   │   └── sharded_client.go        # Application-level sharding (user_id % N)
│   ├── cache/
│   │   └── redis.go                 # Redis read-aside caching for balances
│   ├── ledger/
│   │   └── batcher.go               # Write batching engine (configurable size/interval)
│   └── queue/
│       └── worker.go                # Async worker pool (goroutine-based)
├── scripts/
│   └── benchmark.go                 # Multi-scenario benchmark suite with P50/P95/P99
├── init.sql                         # Database schema + composite indexes
├── docker-compose.yaml              # Infrastructure: 2 Postgres shards + Redis
├── Dockerfile                       # Multi-stage Go build
├── go.mod                           # Go module definition
└── go.sum                           # Dependency checksums
```

---

## 🚀 Getting Started

### Prerequisites

- **Go** 1.24+
- **PostgreSQL** 15+
- **Redis** 7+

### 1. Create Databases & Apply Schema

```bash
# Create two shard databases
psql -U victor -d postgres -c "CREATE DATABASE ledger_shard_1;"
psql -U victor -d postgres -c "CREATE DATABASE ledger_shard_2;"

# Apply schema with indexes to both shards
psql -U victor -d ledger_shard_1 -f init.sql
psql -U victor -d ledger_shard_2 -f init.sql
```

### 2. Start the Server

```bash
export DB_SHARD_1_URL="postgres://victor@localhost:5432/ledger_shard_1?sslmode=disable"
export DB_SHARD_2_URL="postgres://victor@localhost:5432/ledger_shard_2?sslmode=disable"
export REDIS_URL="localhost:6379"

go run main.go
```

On startup the server will:
- Connect to both database shards
- Seed 400 test accounts (200 senders × 200 receivers) across shards
- Pre-warm Redis cache with all account balances
- Start the batcher and worker pool
- Listen on `:8080`

### 3. Run the Benchmark Suite

```bash
go run scripts/benchmark.go
```

This runs 4 scenarios automatically: **Baseline → Medium → High → Burst**, then outputs a summary table with TPS, P50/P95/P99 latencies, and error rates.

### 4. Docker (Optional)

```bash
docker compose up -d --build
```

---

## 📡 API Reference

| Method | Endpoint | Description | Response |
|:------:|:---------|:------------|:---------|
| `POST` | `/transfer` | Submit a financial transaction | `202 Accepted` — queued for async processing |
| `GET` | `/balance?account_id=X&user_id=Y` | Check account balance | JSON with balance + source (cache/db) |
| `GET` | `/health` | Service health check | `{"status":"healthy"}` |
| `GET` | `/stats` | Live system statistics | Accepted count, goroutines, CPUs |

### Transfer Request Body

```json
{
  "id": "tx-001",
  "from_account_id": "acc-0-from",
  "to_account_id": "acc-0-to",
  "amount": 10000,
  "user_id": 0
}
```

### Example cURL

```bash
# Submit a transfer
curl -X POST http://localhost:8080/transfer \
  -H "Content-Type: application/json" \
  -d '{"from_account_id":"acc-0-from","to_account_id":"acc-0-to","amount":100,"user_id":0}'

# Check balance (cached)
curl "http://localhost:8080/balance?account_id=acc-0-from&user_id=0"

# Check system stats
curl http://localhost:8080/stats
```

---

## 🔧 Configuration

| Environment Variable | Default | Description |
|:--------------------|:--------|:------------|
| `DB_SHARD_1_URL` | `postgres://victor@localhost:5432/ledger_shard_1?sslmode=disable` | Postgres shard 1 connection |
| `DB_SHARD_2_URL` | `postgres://victor@localhost:5432/ledger_shard_2?sslmode=disable` | Postgres shard 2 connection |
| `REDIS_URL` | `localhost:6379` | Redis server address |

### Tunable Parameters (in `main.go`)

| Parameter | Default | Description |
|:----------|:--------|:------------|
| `batchSize` | `100` | Number of transactions per batch commit |
| `flushInterval` | `50ms` | Maximum wait time before flushing a partial batch |
| `numWorkers` | `NumCPU * 2` | Number of async processing goroutines |

---

## 📝 License

This project is for educational and benchmarking purposes within a fintech team context.
