// Package periodic provides a small helper for ticker-driven background
// maintenance loops (e.g. periodic eviction of expired in-memory entries).
package periodic

import "time"

// RunCleanup runs purge on every tick of ticker until stop is closed (or
// receives a value), then stops the ticker and returns. It is intended to run
// in its own goroutine. The caller owns the ticker and the stop channel; purge
// is responsible for its own locking.
//
// Calling ticker.Stop() again from the caller's shutdown path is safe:
// time.Ticker.Stop is idempotent.
//
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: run a purge callback on each ticker tick until stopped (mutates shared state)
func RunCleanup(ticker *time.Ticker, stop <-chan bool, purge func()) {
	for {
		select {
		case <-ticker.C:
			purge()
		case <-stop:
			ticker.Stop()
			return
		}
	}
}
