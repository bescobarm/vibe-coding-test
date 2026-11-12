package policy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

type fakeStore struct {
	mu         sync.Mutex
	latest     map[string]Document
	loads      atomic.Int32
	loadErr    error
	publishV   int
	publishErr error
	beforeLoad func() // runs inside LatestPolicy, after the stored doc was read
}

func (f *fakeStore) LatestPolicy(_ context.Context, id string) (Document, error) {
	f.loads.Add(1)
	if f.loadErr != nil {
		return Document{}, f.loadErr
	}
	f.mu.Lock()
	d, ok := f.latest[id]
	f.mu.Unlock()
	if f.beforeLoad != nil {
		f.beforeLoad()
	}
	if !ok {
		return Document{}, domain.ErrNotFound
	}
	return d, nil
}

func (f *fakeStore) PublishPolicy(_ context.Context, d Document) (int, error) {
	if f.publishErr != nil {
		return 0, f.publishErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.publishV++
	d.Version = f.publishV
	f.latest[d.CompanyID] = d
	return f.publishV, nil
}

func newFakeStore(docs ...Document) *fakeStore {
	f := &fakeStore{latest: map[string]Document{}}
	for _, d := range docs {
		f.latest[d.CompanyID] = d
		f.publishV = max(f.publishV, d.Version)
	}
	return f
}

func TestCacheLoadsAPolicyOnlyOnce(t *testing.T) {
	c := require.New(t)
	store := newFakeStore(Document{CompanyID: "acme-mx", Version: 1})
	cache := NewCache(store)

	first, err := cache.Policy(context.Background(), "acme-mx")
	c.NoError(err)
	second, err := cache.Policy(context.Background(), "acme-mx")
	c.NoError(err)

	c.Equal(1, first.Version)
	c.Equal(first, second)
	c.Equal(int32(1), store.loads.Load())
}

func TestCacheReturnsNotFoundWithoutCachingIt(t *testing.T) {
	c := require.New(t)
	store := newFakeStore()
	cache := NewCache(store)

	_, err := cache.Policy(context.Background(), "nobody")
	c.ErrorIs(err, domain.ErrNotFound)
	_, err = cache.Policy(context.Background(), "nobody")
	c.ErrorIs(err, domain.ErrNotFound)

	c.Equal(int32(2), store.loads.Load())
}

func TestCachePropagatesStoreErrors(t *testing.T) {
	c := require.New(t)
	boom := errors.New("boom")
	cache := NewCache(&fakeStore{loadErr: boom})

	_, err := cache.Policy(context.Background(), "acme-mx")

	c.ErrorIs(err, boom)
}

func TestPublishIsVisibleOnTheNextRequest(t *testing.T) {
	c := require.New(t)
	store := newFakeStore(Document{CompanyID: "acme-mx", Version: 1})
	cache := NewCache(store)
	_, err := cache.Policy(context.Background(), "acme-mx")
	c.NoError(err)

	published, err := cache.Publish(context.Background(), Document{CompanyID: "acme-mx", Controls: Controls{BlockedMCCs: []string{"7995"}}})
	c.NoError(err)
	got, err := cache.Policy(context.Background(), "acme-mx")

	c.NoError(err)
	c.Equal(2, published.Version)
	c.Equal(2, got.Version)
	c.Equal([]string{"7995"}, got.Controls.BlockedMCCs)
	c.Equal(int32(1), store.loads.Load(), "a publish must not cause a reload")
}

func TestPublishPropagatesStoreErrors(t *testing.T) {
	c := require.New(t)
	cache := NewCache(&fakeStore{latest: map[string]Document{}, publishErr: domain.ErrConflict})

	_, err := cache.Publish(context.Background(), Document{CompanyID: "acme-mx"})

	c.ErrorIs(err, domain.ErrConflict)
}

// A first load that read v1 from the store must not overwrite a v2 that was
// published while the load was in flight.
func TestASlowLoadDoesNotUndoAPublish(t *testing.T) {
	c := require.New(t)
	store := newFakeStore(Document{CompanyID: "acme-mx", Version: 1})
	cache := NewCache(store)
	store.beforeLoad = func() {
		store.beforeLoad = nil
		_, err := cache.Publish(context.Background(), Document{CompanyID: "acme-mx"})
		c.NoError(err)
	}

	got, err := cache.Policy(context.Background(), "acme-mx")

	c.NoError(err)
	c.Equal(2, got.Version)
	again, err := cache.Policy(context.Background(), "acme-mx")
	c.NoError(err)
	c.Equal(2, again.Version)
}

func TestConcurrentReadsAndPublishesAreRaceFree(t *testing.T) {
	c := require.New(t)
	store := newFakeStore(Document{CompanyID: "acme-mx", Version: 1}, Document{CompanyID: "beta-co", Version: 1})
	cache := NewCache(store)

	var wg sync.WaitGroup
	errs := make([]error, 100)
	for i := range 100 {
		wg.Go(func() {
			switch i % 10 {
			case 0:
				_, errs[i] = cache.Publish(context.Background(), Document{CompanyID: "acme-mx"})
			case 1:
				_, errs[i] = cache.Policy(context.Background(), "beta-co")
			default:
				_, errs[i] = cache.Policy(context.Background(), "acme-mx")
			}
		})
	}
	wg.Wait()

	for i := range errs {
		c.NoError(errs[i], "call %d", i)
	}
	got, err := cache.Policy(context.Background(), "acme-mx")
	c.NoError(err)
	c.Equal(11, got.Version, "v1 plus 10 publishes")
}
