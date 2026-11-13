package policy

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
)

// Store persists policy versions. LatestPolicy returns domain.ErrNotFound when
// the company has no policy.
type Store interface {
	LatestPolicy(ctx context.Context, companyID string) (Document, error)
	PublishPolicy(ctx context.Context, doc Document) (version int, err error)
}

// Cache keeps the latest policy of each company in memory (D-037).
//
// Readers load an immutable map through an atomic pointer and never take a
// lock. Writers (a publish, or a first load) copy the map, change the copy and
// swap the pointer; mu only serializes writers. A cached version is never
// replaced by an older one, so a slow first load can't undo a publish.
type Cache struct {
	store Store
	mu    sync.Mutex
	docs  atomic.Pointer[map[string]Document]
}

// NewCache returns an empty cache backed by store.
func NewCache(store Store) *Cache {
	c := &Cache{store: store}
	c.docs.Store(&map[string]Document{})
	return c
}

// Policy returns the company's latest policy, loading it from the store on the first request.
func (c *Cache) Policy(ctx context.Context, companyID string) (Document, error) {
	cached, ok := (*c.docs.Load())[companyID]
	if ok {
		return cached, nil
	}

	d, err := c.store.LatestPolicy(ctx, companyID)
	if err != nil {
		return Document{}, err
	}

	return c.put(d), nil
}

// Publish stores a new version and makes it visible to the next request.
// The caller must have validated doc.
func (c *Cache) Publish(ctx context.Context, doc Document) (Document, error) {
	v, err := c.store.PublishPolicy(ctx, doc)
	if err != nil {
		return Document{}, err
	}

	doc.Version = v
	return c.put(doc), nil
}

// put stores d unless a newer version is already cached, and returns the cached document.
func (c *Cache) put(d Document) Document {
	c.mu.Lock()
	defer c.mu.Unlock()

	current := *c.docs.Load()
	cached, ok := current[d.CompanyID]
	if ok && cached.Version >= d.Version {
		return cached
	}

	next := maps.Clone(current)
	next[d.CompanyID] = d
	c.docs.Store(&next)
	return d
}
