package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

type ShardedClient struct {
	Shards []*sql.DB
}

func NewShardedClient(urls []string) (*ShardedClient, error) {
	shards := make([]*sql.DB, len(urls))
	for i, url := range urls {
		db, err := sql.Open("postgres", url)
		if err != nil {
			return nil, fmt.Errorf("failed to open shard %d: %w", i, err)
		}
		if err := db.Ping(); err != nil {
			log.Printf("Warning: shard %d not reachable yet: %v", i, err)
		}
		shards[i] = db
	}
	return &ShardedClient{Shards: shards}, nil
}

// GetShard returns the shard for a given user ID using simple modulo sharding
func (s *ShardedClient) GetShard(userID int) *sql.DB {
	shardIndex := userID % len(s.Shards)
	return s.Shards[shardIndex]
}

func (s *ShardedClient) Close() {
	for _, shard := range s.Shards {
		shard.Close()
	}
}
