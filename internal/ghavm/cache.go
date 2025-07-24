package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/mccutchen/ghavm/internal/slogctx"
)

type entry[V any] struct {
	val V
	err error
}

// Cache is a dumb map-based concurrency-safe in-memory cache, useful for
// short-lived processes.
type Cache[K comparable, V any] struct {
	mu    sync.Mutex
	cache map[K]entry[V]
}

// Do caches the result of calling thunk.
func (c *Cache[K, V]) Do(ctx context.Context, key K, thunk func() (V, error)) (V, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[K]entry[V])
	}
	if entry, found := c.cache[key]; found {
		slogctx.Debug(ctx, "cache: hit", slog.Any("key", key))
		return entry.val, entry.err
	}
	slogctx.Debug(ctx, "cache: miss", slog.Any("key", key))
	val, err := thunk()
	c.cache[key] = entry[V]{val, err}
	return val, err
}
