package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"high_throughput_system/internal/cache"
	"high_throughput_system/internal/db"
	"high_throughput_system/internal/ledger"
	"high_throughput_system/internal/queue"
	"log"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/valyala/fasthttp"
)

type App struct {
	dbClient    *db.ShardedClient
	cache       *cache.CacheClient
	batcher     *ledger.Batcher
	workerPool  *queue.WorkerPool
	txProcessed int64
	txAccepted  int64
}

func main() {
	dbShard1 := getEnvDefault("DB_SHARD_1_URL", "postgres://victor@localhost:5432/ledger_shard_1?sslmode=disable")
	dbShard2 := getEnvDefault("DB_SHARD_2_URL", "postgres://victor@localhost:5432/ledger_shard_2?sslmode=disable")
	redisURL := getEnvDefault("REDIS_URL", "localhost:6379")
	batchSize := 100
	flushInterval := 50 * time.Millisecond
	numWorkers := runtime.NumCPU() * 2

	log.Printf("🚀 High-Throughput Ledger Engine")
	log.Printf("   CPU Cores:       %d", runtime.NumCPU())
	log.Printf("   Workers:         %d", numWorkers)
	log.Printf("   Batch Size:      %d", batchSize)
	log.Printf("   Flush Interval:  %v", flushInterval)
	log.Printf("   DB Shard 1:      %s", dbShard1)
	log.Printf("   DB Shard 2:      %s", dbShard2)
	log.Printf("   Redis:           %s", redisURL)

	shardedClient, err := db.NewShardedClient([]string{dbShard1, dbShard2})
	if err != nil {
		log.Fatalf("Failed to initialize database shards: %v", err)
	}
	defer shardedClient.Close()

	cacheClient := cache.NewCacheClient(redisURL)

	// Seed test accounts across shards
	seedAccounts(shardedClient, cacheClient)

	batcher := ledger.NewBatcher(shardedClient, batchSize, flushInterval)
	workerPool := queue.NewWorkerPool(numWorkers, batcher)

	app := &App{
		dbClient:   shardedClient,
		cache:      cacheClient,
		batcher:    batcher,
		workerPool: workerPool,
	}

	batcher.Start()
	workerPool.Start()

	// Print live stats every 5 seconds
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			accepted := atomic.LoadInt64(&app.txAccepted)
			log.Printf("📊 Live Stats — Accepted: %d", accepted)
		}
	}()

	requestHandler := func(ctx *fasthttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/transfer":
			app.handleTransfer(ctx)
		case "/balance":
			app.handleBalance(ctx)
		case "/health":
			app.handleHealth(ctx)
		case "/stats":
			app.handleStats(ctx)
		default:
			ctx.Error("Not Found", fasthttp.StatusNotFound)
		}
	}

	server := &fasthttp.Server{
		Handler:            requestHandler,
		MaxRequestBodySize: 4 * 1024 * 1024,
		Concurrency:        256 * 1024,
		ReadBufferSize:     4096,
		WriteBufferSize:    4096,
	}

	log.Println("✅ Server starting on :8080...")
	if err := server.ListenAndServe(":8080"); err != nil {
		log.Fatalf("Error in ListenAndServe: %v", err)
	}
}

// ────────────────────────────────────────────────────────────────
// Request types
// ────────────────────────────────────────────────────────────────

type TransferRequest struct {
	ID            string `json:"id"`
	FromAccountID string `json:"from_account_id"`
	ToAccountID   string `json:"to_account_id"`
	Amount        int64  `json:"amount"`
	UserID        int    `json:"user_id"`
}

// ────────────────────────────────────────────────────────────────
// Handlers
// ────────────────────────────────────────────────────────────────

func (app *App) handleTransfer(ctx *fasthttp.RequestCtx) {
	var req TransferRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		ctx.Error(`{"error":"invalid json"}`, fasthttp.StatusBadRequest)
		return
	}

	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	// Strategy: Caching — Quick balance check from Redis
	balance, err := app.cache.GetBalance(context.Background(), req.FromAccountID)
	if err == nil && balance < req.Amount {
		ctx.Error(`{"error":"insufficient balance"}`, fasthttp.StatusBadRequest)
		return
	}

	// Strategy: Async Queuing — Decouple acceptance from processing
	app.workerPool.Submit(ledger.Transaction{
		ID:            req.ID,
		FromAccountID: req.FromAccountID,
		ToAccountID:   req.ToAccountID,
		Amount:        req.Amount,
		UserID:        req.UserID,
	})

	atomic.AddInt64(&app.txAccepted, 1)

	ctx.SetStatusCode(fasthttp.StatusAccepted)
	ctx.SetBodyString(`{"status":"accepted"}`)
}

func (app *App) handleBalance(ctx *fasthttp.RequestCtx) {
	accountID := string(ctx.QueryArgs().Peek("account_id"))
	userIDStr := string(ctx.QueryArgs().Peek("user_id"))
	if accountID == "" {
		ctx.Error(`{"error":"missing account_id"}`, fasthttp.StatusBadRequest)
		return
	}

	// Strategy: Read-aside caching
	balance, err := app.cache.GetBalance(context.Background(), accountID)
	if err == nil {
		ctx.SetBodyString(fmt.Sprintf(`{"account_id":"%s","balance":%d,"source":"cache"}`, accountID, balance))
		return
	}

	// Cache miss — read from DB
	if userIDStr != "" {
		var uid int
		fmt.Sscanf(userIDStr, "%d", &uid)
		shard := app.dbClient.GetShard(uid)
		var dbBalance int64
		err := shard.QueryRow("SELECT balance FROM accounts WHERE id = $1", accountID).Scan(&dbBalance)
		if err == nil {
			// Populate cache for next time
			_ = app.cache.SetBalance(context.Background(), accountID, dbBalance)
			ctx.SetBodyString(fmt.Sprintf(`{"account_id":"%s","balance":%d,"source":"db"}`, accountID, dbBalance))
			return
		}
	}

	ctx.Error(`{"error":"account not found"}`, fasthttp.StatusNotFound)
}

func (app *App) handleHealth(ctx *fasthttp.RequestCtx) {
	ctx.SetBodyString(`{"status":"healthy","service":"high-throughput-ledger"}`)
}

func (app *App) handleStats(ctx *fasthttp.RequestCtx) {
	accepted := atomic.LoadInt64(&app.txAccepted)
	ctx.SetBodyString(fmt.Sprintf(`{"accepted":%d,"goroutines":%d,"cpus":%d}`,
		accepted, runtime.NumGoroutine(), runtime.NumCPU()))
}

// ────────────────────────────────────────────────────────────────
// Seed helpers
// ────────────────────────────────────────────────────────────────

func seedAccounts(client *db.ShardedClient, cacheClient *cache.CacheClient) {
	log.Println("🌱 Seeding test accounts across shards...")
	ctx := context.Background()
	for i := 0; i < 200; i++ {
		shard := client.GetShard(i)
		fromID := fmt.Sprintf("acc-%d-from", i)
		toID := fmt.Sprintf("acc-%d-to", i)
		insertAccount(shard, fromID, i, 1_000_000_00) // $1M in cents
		insertAccount(shard, toID, i, 0)
		_ = cacheClient.SetBalance(ctx, fromID, 1_000_000_00)
		_ = cacheClient.SetBalance(ctx, toID, 0)
	}
	log.Println("✅ Seeded 400 accounts (200 senders, 200 receivers)")
}

func insertAccount(shard *sql.DB, id string, userID int, balance int64) {
	_, _ = shard.Exec(
		`INSERT INTO accounts (id, user_id, balance, currency)
		 VALUES ($1, $2, $3, 'USD')
		 ON CONFLICT (id) DO UPDATE SET balance = $3`,
		id, userID, balance,
	)
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
