package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct{ client *redis.Client }

func NewCache(url string) (*RedisCache, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis.ParseURL: %w", err)
	}
	client := redis.NewClient(opt)
	return &RedisCache{client: client}, nil
}

func (c *RedisCache) GetJob(ctx context.Context, id string) (*JobSnapshot, error) {
	val, err := c.client.Get(ctx, "job:"+id).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var j JobSnapshot
	if err := json.Unmarshal([]byte(val), &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}
