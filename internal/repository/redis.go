package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/models"

	"github.com/redis/go-redis/v9"
)

type RedisStateRepository struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisClient создает новый клиент Redis на основе конфигурации
func NewRedisClient(cfg config.RedisConfig) *redis.Client {
	options := &redis.Options{
		Addr:     cfg.Address,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	}

	client := redis.NewClient(options)

	return client
}

func NewRedisStateRepository(client *redis.Client, ttl time.Duration) *RedisStateRepository {
	return &RedisStateRepository{
		client: client,
		ttl:    ttl,
	}
}

func (r *RedisStateRepository) GetState(ctx context.Context, userID int64) (*models.UserState, error) {
	if r.client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}
	key := fmt.Sprintf("user_state:%d", userID)
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get state from redis: %w", err)
	}

	var state models.UserState
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

func (r *RedisStateRepository) SetState(ctx context.Context, state *models.UserState) error {
	if r.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	key := fmt.Sprintf("user_state:%d", state.UserID)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := r.client.Set(ctx, key, data, r.ttl).Err(); err != nil {
		return fmt.Errorf("failed to set state in redis: %w", err)
	}

	return nil
}

func (r *RedisStateRepository) ClearState(ctx context.Context, userID int64) error {
	if r.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	key := fmt.Sprintf("user_state:%d", userID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete state from redis: %w", err)
	}
	return nil
}

func (r *RedisStateRepository) CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	if r.client == nil {
		return false, fmt.Errorf("redis client is nil")
	}
	key := fmt.Sprintf("rate_limit:%d", userID)
	count, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to increment rate limit: %w", err)
	}

	if count == 1 {
		r.client.Expire(ctx, key, window)
	}

	return count <= int64(limit), nil
}

// Ping проверяет соединение с Redis
func Ping(ctx context.Context, client *redis.Client) error {
	_, err := client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to ping Redis: %w", err)
	}
	return nil
}

// Close закрывает соединение с Redis
func Close(client *redis.Client) error {
	if client != nil {
		return client.Close()
	}
	return nil
}
