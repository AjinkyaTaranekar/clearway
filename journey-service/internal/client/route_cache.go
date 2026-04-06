package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRouteCache caches Map Service responses in Redis
type RedisRouteCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisRouteCache creates a new route cache.
// If client is nil, all operations are no-ops (cache disabled).
func NewRedisRouteCache(client *redis.Client, ttlHours int) *RedisRouteCache {
	return &RedisRouteCache{
		client: client,
		ttl:    time.Duration(ttlHours) * time.Hour,
	}
}

func cacheKey(origin, dest MapCoordinates) string {
	return fmt.Sprintf("route:%.3f:%.3f:%.3f:%.3f",
		roundTo3(origin.Lat), roundTo3(origin.Lng),
		roundTo3(dest.Lat), roundTo3(dest.Lng))
}

func roundTo3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

// Get retrieves a cached route. Returns nil if not found or cache disabled.
func (c *RedisRouteCache) Get(ctx context.Context, origin, dest MapCoordinates) (*RouteResponse, error) {
	if c.client == nil {
		return nil, nil
	}
	key := cacheKey(origin, dest)
	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, nil // treat redis errors as cache miss
	}
	var route RouteResponse
	if err := json.Unmarshal(data, &route); err != nil {
		return nil, nil
	}
	return &route, nil
}

// Set stores a route in the cache. Silently ignores errors.
func (c *RedisRouteCache) Set(ctx context.Context, origin, dest MapCoordinates, route *RouteResponse) error {
	if c.client == nil {
		return nil
	}
	data, err := json.Marshal(route)
	if err != nil {
		return nil
	}
	key := cacheKey(origin, dest)
	c.client.Set(ctx, key, data, c.ttl)
	return nil
}
