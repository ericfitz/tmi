package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// DefaultPruneInterval is the default interval between pruning runs.
const DefaultPruneInterval = 24 * time.Hour

// AuditPruner runs periodic cleanup of expired audit entries and version snapshots.
type AuditPruner struct {
	auditService AuditServiceInterface
	interval     time.Duration
	cancel       context.CancelFunc
}

// NewAuditPruner creates a new pruner for the given audit service.
func NewAuditPruner(auditService AuditServiceInterface) *AuditPruner {
	return &AuditPruner{
		auditService: auditService,
		interval:     DefaultPruneInterval,
	}
}

// Start begins the background pruning goroutine.
func (p *AuditPruner) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go p.run(ctx)
	slogging.Get().Info("audit pruner started with interval %s", p.interval)
}

// Stop gracefully stops the pruning goroutine.
func (p *AuditPruner) Stop() {
	if p.cancel != nil {
		p.cancel()
		slogging.Get().Info("audit pruner stopped")
	}
}

// run is the main pruning loop.
func (p *AuditPruner) run(ctx context.Context) {
	// Run once at startup (with a short delay to let the server finish initializing)
	timer := time.NewTimer(1 * time.Minute)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			p.prune(ctx)
			timer.Reset(p.interval)
		}
	}
}

// prune executes one pruning cycle.
func (p *AuditPruner) prune(ctx context.Context) {
	logger := slogging.Get()

	// Prune version snapshots first (they reference audit entries)
	snapshotsPruned, err := p.auditService.PruneVersionSnapshots(ctx)
	if err != nil {
		logger.Error("failed to prune version snapshots: %v", err)
	} else if snapshotsPruned > 0 {
		logger.Info("pruned %d version snapshots", snapshotsPruned)
	}

	// Then prune audit entries
	entriesPruned, err := p.auditService.PruneAuditEntries(ctx)
	if err != nil {
		logger.Error("failed to prune audit entries: %v", err)
	} else if entriesPruned > 0 {
		logger.Info("pruned %d audit entries", entriesPruned)
	}

	// Purge expired tombstones (soft-deleted entities past retention period)
	tombstonesPurged, err := p.auditService.PurgeTombstones(ctx)
	if err != nil {
		logger.Error("failed to purge tombstones: %v", err)
	} else if tombstonesPurged > 0 {
		logger.Info("purged %d expired tombstones", tombstonesPurged)
	}
}
