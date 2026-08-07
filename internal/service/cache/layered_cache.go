package cache

import (
	"context"
	"errors"
	"time"
)

// LayeredCache checks a fast local cache before a shared cache.
//
// It is intended for L0 memory exact cache + L1 Redis exact cache. Set writes
// to both layers, while Get does not promote L1 hits into L0 because Entry does
// not currently carry remaining TTL information.
type LayeredCache struct {
	l0 Cache
	l1 Cache
}

func NewLayeredCache(l0, l1 Cache) *LayeredCache {
	return &LayeredCache{l0: l0, l1: l1}
}

func (c *LayeredCache) Get(ctx context.Context, key string) (*Entry, bool, error) {
	var firstErr error
	if c.l0 != nil {
		entry, hit, err := c.l0.Get(ctx, key)
		if hit {
			return entry, true, nil
		}
		firstErr = err
	}
	if c.l1 != nil {
		entry, hit, err := c.l1.Get(ctx, key)
		if hit || err != nil {
			return entry, hit, err
		}
	}
	return nil, false, firstErr
}

func (c *LayeredCache) Set(ctx context.Context, key string, e *Entry, ttl time.Duration) error {
	var errs []error
	if c.l0 != nil {
		if err := c.l0.Set(ctx, key, e, ttl); err != nil {
			errs = append(errs, err)
		}
	}
	if c.l1 != nil {
		if err := c.l1.Set(ctx, key, e, ttl); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *LayeredCache) Delete(ctx context.Context, key string) error {
	if c.l0 != nil {
		_ = c.l0.Delete(ctx, key)
	}
	if c.l1 != nil {
		_ = c.l1.Delete(ctx, key)
	}
	return nil
}

func (c *LayeredCache) Stats() Stats {
	if c.l0 != nil {
		return c.l0.Stats()
	}
	if c.l1 != nil {
		return c.l1.Stats()
	}
	return Stats{}
}

func (c *LayeredCache) Close() error {
	if c.l0 != nil {
		_ = c.l0.Close()
	}
	if c.l1 != nil {
		_ = c.l1.Close()
	}
	return nil
}
