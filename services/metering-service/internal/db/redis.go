package db

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisRepo struct {
	client *redis.Client
}

func NewRedisRepo(url string) *RedisRepo {
	return &RedisRepo{
		client: redis.NewClient(&redis.Options{
			Addr: url,
		}),
	}
}

func (r *RedisRepo) UpdateRealtimeBudget(ctx context.Context, agentID string, amount float64, currency string) error {
	key := fmt.Sprintf("budget:%s:%s", agentID, currency)
	
	// Increment total spent for the day (simple example)
	today := time.Now().Format("2006-01-02")
	dailyKey := fmt.Sprintf("%s:%s", key, today)

	pipe := r.client.Pipeline()
	pipe.IncrByFloat(ctx, key, amount)
	pipe.IncrByFloat(ctx, dailyKey, amount)
	pipe.Expire(ctx, dailyKey, 25*time.Hour) // Keep for slightly more than a day

	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisRepo) GetRemainingBudget(ctx context.Context, agentID string, currency string) (float64, error) {
	key := fmt.Sprintf("budget:%s:%s", agentID, currency)
	val, err := r.client.Get(ctx, key).Float64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}
