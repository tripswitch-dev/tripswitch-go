package tripswitch

import (
	"context"
	"errors"
	"time"
)

// startMetadataSync runs the background metadata refresh goroutine.
// The caller must call c.wg.Add(1) before launching this goroutine.
func (c *Client) startMetadataSync() {
	defer c.wg.Done()

	// Initial fetch — best effort; stop if auth fails
	if c.refreshMetadata(c.ctx) {
		return
	}

	ticker := time.NewTicker(c.metaSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.refreshMetadata(c.ctx) {
				return
			}
		}
	}
}

// refreshMetadata fetches breaker and router metadata, using ETags for
// conditional requests. On 304 Not Modified the cache is left unchanged.
// Returns true if the caller should stop syncing (e.g., on auth failure).
func (c *Client) refreshMetadata(ctx context.Context) (stop bool) {
	c.metaMu.RLock()
	bETag := c.breakersETag
	rETag := c.routersETag
	c.metaMu.RUnlock()

	// Fetch breakers metadata with its own timeout
	bCtx, bCancel := context.WithTimeout(ctx, 10*time.Second)
	breakers, newBETag, err := c.ListBreakersMetadata(bCtx, bETag)
	bCancel()
	if errors.Is(err, ErrUnauthorized) {
		c.logger.Warn("metadata sync stopping due to auth failure")
		return true
	}
	if err != nil && !errors.Is(err, ErrNotModified) {
		c.logger.Warn("failed to refresh breakers metadata", "error", err)
	}
	if err == nil {
		c.metaMu.Lock()
		c.breakersMeta = breakers
		c.breakersETag = newBETag
		c.metaMu.Unlock()
	}

	// Fetch routers metadata with its own timeout
	rCtx, rCancel := context.WithTimeout(ctx, 10*time.Second)
	routers, newRETag, err := c.ListRoutersMetadata(rCtx, rETag)
	rCancel()
	if errors.Is(err, ErrUnauthorized) {
		c.logger.Warn("metadata sync stopping due to auth failure")
		return true
	}
	if err != nil && !errors.Is(err, ErrNotModified) {
		c.logger.Warn("failed to refresh routers metadata", "error", err)
	}
	if err == nil {
		c.metaMu.Lock()
		c.routersMeta = routers
		c.routersETag = newRETag
		c.metaMu.Unlock()
	}

	return false
}

// GetBreakersMetadata returns a deep copy of the cached breaker metadata.
func (c *Client) GetBreakersMetadata() []BreakerMeta {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	if c.breakersMeta == nil {
		return nil
	}
	result := make([]BreakerMeta, len(c.breakersMeta))
	for i, b := range c.breakersMeta {
		result[i] = BreakerMeta{ID: b.ID, Name: b.Name}
		if b.Metadata != nil {
			result[i].Metadata = make(map[string]string, len(b.Metadata))
			for k, v := range b.Metadata {
				result[i].Metadata[k] = v
			}
		}
	}
	return result
}

// GetRoutersMetadata returns a deep copy of the cached router metadata.
func (c *Client) GetRoutersMetadata() []RouterMeta {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	if c.routersMeta == nil {
		return nil
	}
	result := make([]RouterMeta, len(c.routersMeta))
	for i, r := range c.routersMeta {
		result[i] = RouterMeta{ID: r.ID, Name: r.Name}
		if r.Metadata != nil {
			result[i].Metadata = make(map[string]string, len(r.Metadata))
			for k, v := range r.Metadata {
				result[i].Metadata[k] = v
			}
		}
	}
	return result
}
