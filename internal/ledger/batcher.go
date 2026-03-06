package ledger

import (
	"context"
	"high_throughput_system/internal/db"
	"log"
	"sync"
	"time"
)

type Transaction struct {
	ID            string
	FromAccountID string
	ToAccountID   string
	Amount        int64
	UserID        int // Used for sharding
}

type Batcher struct {
	client    *db.ShardedClient
	batchSize int
	flushFreq time.Duration
	tasks     chan Transaction
	wg        sync.WaitGroup
	quit      chan struct{}
}

func NewBatcher(client *db.ShardedClient, batchSize int, flushFreq time.Duration) *Batcher {
	return &Batcher{
		client:    client,
		batchSize: batchSize,
		flushFreq: flushFreq,
		tasks:     make(chan Transaction, 10000),
		quit:      make(chan struct{}),
	}
}

func (b *Batcher) Start() {
	b.wg.Add(1)
	go b.run()
}

func (b *Batcher) Stop() {
	close(b.quit)
	b.wg.Wait()
}

func (b *Batcher) Add(tx Transaction) {
	b.tasks <- tx
}

func (b *Batcher) run() {
	defer b.wg.Done()

	// Separate batches for each shard
	shardBatches := make([][]Transaction, len(b.client.Shards))
	ticker := time.NewTicker(b.flushFreq)
	defer ticker.Stop()

	for {
		select {
		case tx := <-b.tasks:
			shardIndex := tx.UserID % len(b.client.Shards)
			shardBatches[shardIndex] = append(shardBatches[shardIndex], tx)
			if len(shardBatches[shardIndex]) >= b.batchSize {
				b.flush(shardIndex, shardBatches[shardIndex])
				shardBatches[shardIndex] = nil
			}
		case <-ticker.C:
			for i := range shardBatches {
				if len(shardBatches[i]) > 0 {
					b.flush(i, shardBatches[i])
					shardBatches[i] = nil
				}
			}
		case <-b.quit:
			for i := range shardBatches {
				if len(shardBatches[i]) > 0 {
					b.flush(i, shardBatches[i])
				}
			}
			return
		}
	}
}

func (b *Batcher) flush(shardIndex int, txs []Transaction) {
	dbShard := b.client.Shards[shardIndex]
	ctx := context.Background()
	tx, err := dbShard.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("Failed to begin transaction on shard %d: %v", shardIndex, err)
		return
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT INTO transactions (id, from_account_id, to_account_id, amount, status) VALUES ($1, $2, $3, $4, 'completed')")
	if err != nil {
		log.Printf("Failed to prepare statement on shard %d: %v", shardIndex, err)
		return
	}
	defer stmt.Close()

	for _, t := range txs {
		if _, err := stmt.ExecContext(ctx, t.ID, t.FromAccountID, t.ToAccountID, t.Amount); err != nil {
			log.Printf("Failed to execute statement on shard %d: %v", shardIndex, err)
			return
		}

		// Update balances (simplified)
		_, _ = tx.ExecContext(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", t.Amount, t.FromAccountID)
		_, _ = tx.ExecContext(ctx, "UPDATE accounts SET balance = balance + $1 WHERE id = $2", t.Amount, t.ToAccountID)
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit transaction on shard %d: %v", shardIndex, err)
	}
}
