package api

import (
	"context"
	"sync"

	"golang.org/x/sync/semaphore"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/pkg/extract"
)

// ConcurrencyLimiter caps simultaneous extractions per user. Capacity is
// looked up on first acquire and cached per-user for the lifetime of the
// process (override changes don't resize the existing semaphore — known
// limitation, see design spec). The lookup callback is invoked while the
// internal mutex is held, so callers must supply a fast (cached) lookup.
// SEM@d1fd850907490887fd11a6ccd4a691326ede6e4e: per-user weighted semaphore pool capping simultaneous content extractions (mutates shared state)
type ConcurrencyLimiter struct {
	mu       sync.Mutex
	sems     map[string]*semaphore.Weighted
	lookup   func(ctx context.Context, userID string) (int, error)
	fallback int
}

// NewConcurrencyLimiter is the public constructor used by server wiring.
// fallback is the per-user concurrency cap used when no override is set;
// lookup is called on first acquire per user to fetch the override value.
// A nil lookup means "always use fallback". Values outside (0,
// config.MaxPerUserConcurrency] are clamped to the safe default of 2.
// SEM@d1fd850907490887fd11a6ccd4a691326ede6e4e: build a ConcurrencyLimiter with a fallback cap and optional per-user override lookup (pure)
func NewConcurrencyLimiter(fallback int, lookup func(ctx context.Context, userID string) (int, error)) *ConcurrencyLimiter {
	if fallback <= 0 || fallback > config.MaxPerUserConcurrency {
		fallback = 2
	}
	return &ConcurrencyLimiter{
		sems:     map[string]*semaphore.Weighted{},
		lookup:   lookup,
		fallback: fallback,
	}
}

// SEM@d1fd850907490887fd11a6ccd4a691326ede6e4e: acquire a per-user extraction slot, blocking until available or context cancelled (mutates shared state)
func (cl *ConcurrencyLimiter) acquire(ctx context.Context, userID string) (release func(), err error) {
	cl.mu.Lock()
	sem, ok := cl.sems[userID]
	if !ok {
		n := cl.fallback
		if cl.lookup != nil {
			if got, lerr := cl.lookup(ctx, userID); lerr == nil && got > 0 && got <= config.MaxPerUserConcurrency {
				n = got
			}
		}
		sem = semaphore.NewWeighted(int64(n))
		cl.sems[userID] = sem
	}
	cl.mu.Unlock()
	if err := sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	return func() { sem.Release(1) }, nil
}

// OOXMLLimitsFromConfig converts a validated ContentExtractorsConfig into the
// relocated extract.Limits value consumed by the OOXML extractors. Used by
// server wiring; tests can continue to use extract.DefaultLimits().
//
// MaxXMLElementDepth and MaxCompressionRatio are server-only ceilings (not
// operator-tunable) and are populated with the const ceilings here so the
// extractors see consistent values regardless of caller.
// SEM@d1fd850907490887fd11a6ccd4a691326ede6e4e: convert operator content-extractor config to OOXML extractor limits with fixed security ceilings (pure)
func OOXMLLimitsFromConfig(c config.ContentExtractorsConfig) extract.Limits {
	return extract.Limits{
		CompressedSizeBytes:   c.CompressedSizeBytes,
		DecompressedSizeBytes: c.DecompressedSizeBytes,
		PartSizeBytes:         c.PartSizeBytes,
		MarkdownSizeBytes:     c.MarkdownSizeBytes,
		MaxXMLElementDepth:    100,
		MaxCompressionRatio:   100,
		PPTXSlides:            c.PPTXSlides,
		XLSXCells:             c.XLSXCells,
		WallClockBudget:       c.WallClockBudget,
	}
}
