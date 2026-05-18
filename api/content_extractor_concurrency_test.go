package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConcurrencyLimiter_BlocksAndReleases(t *testing.T) {
	cl := NewConcurrencyLimiter(2, func(ctx context.Context, userID string) (int, error) {
		return 0, nil // no override; use fallback
	})
	var concurrent int32
	var maxObserved int32
	var wg sync.WaitGroup
	work := func() {
		release, err := cl.acquire(context.Background(), "alice")
		assert.NoError(t, err)
		defer release()
		n := atomic.AddInt32(&concurrent, 1)
		for {
			cur := atomic.LoadInt32(&maxObserved)
			if n <= cur || atomic.CompareAndSwapInt32(&maxObserved, cur, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); work() }()
	}
	wg.Wait()
	assert.LessOrEqual(t, maxObserved, int32(2), "must never exceed configured limit")
}

func TestConcurrencyLimiter_OverrideHonored(t *testing.T) {
	cl := NewConcurrencyLimiter(2, func(ctx context.Context, userID string) (int, error) {
		if userID == "bot" {
			return 5, nil
		}
		return 0, nil
	})
	release, err := cl.acquire(context.Background(), "bot")
	assert.NoError(t, err)
	release()
	// Internal: confirm cap is 5 by attempting 5 concurrent acquires without timing out.
	var wg sync.WaitGroup
	hold := make(chan struct{})
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			rel, err := cl.acquire(ctx, "bot")
			assert.NoError(t, err)
			<-hold
			rel()
		}()
	}
	close(hold)
	wg.Wait()
}

func TestConcurrencyLimiter_OverrideOutOfBoundFallsBack(t *testing.T) {
	cl := NewConcurrencyLimiter(2, func(ctx context.Context, userID string) (int, error) {
		return 999, nil // out of bounds; must fall back to 2
	})
	rel, err := cl.acquire(context.Background(), "u")
	assert.NoError(t, err)
	rel()
	// Verify by saturating: 3rd acquirer should block until release.
	rel1, _ := cl.acquire(context.Background(), "u")
	rel2, _ := cl.acquire(context.Background(), "u")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = cl.acquire(ctx, "u")
	assert.Error(t, err, "third concurrent acquire must time out under fallback=2")
	rel1()
	rel2()
}

func TestConcurrencyLimiter_LookupErrorFallsBack(t *testing.T) {
	cl := NewConcurrencyLimiter(2, func(ctx context.Context, userID string) (int, error) {
		return 0, errors.New("db down")
	})
	rel, err := cl.acquire(context.Background(), "u")
	assert.NoError(t, err)
	rel()
}
