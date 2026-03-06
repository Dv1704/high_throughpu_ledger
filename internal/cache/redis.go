package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type CacheClient struct {
	Client *redis.Client
}

func NewCacheClient(url string) *CacheClient {
	rdb := redis.NewClient(&redis.Options{
		Addr: url,
	})
	return &CacheClient{Client: rdb}
}

func (c *CacheClient) SetBalance(ctx context.Context, accountID string, balance int64) error {
	key := fmt.Sprintf("balance:%s", accountID)
	return c.Client.Set(ctx, key, balance, 10*time.Minute).Err()
}

func (c *CacheClient) GetBalance(ctx context.Context, accountID string) (int64, error) {
	key := fmt.Sprintf("balance:%s", accountID)
	val, err := c.Client.Get(ctx, key).Int64()
	if err != nil {
		return 0, err
	}
	return val, nil
}

func (c *CacheClient) DeleteBalance(ctx context.Context, accountID string) error {
	key := fmt.Sprintf("balance:%s", accountID)
	return c.Client.Del(ctx, key).Err()
}
