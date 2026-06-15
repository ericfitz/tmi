package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
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

// pruneFailureMessage builds an operator-actionable log message for a prune
// failure. Append-only trigger violations get a specific message: they mean
// the retention config was lowered after boot (the trigger floor is baked in
// at install time) or the floor's hard minimum is above the configured
// retention — both fixed by aligning config and restarting.
func pruneFailureMessage(what string, err error) string {
	classified := dberrors.Classify(err)
	if errors.Is(classified, dberrors.ErrAppendOnlyViolation) || errors.Is(err, dberrors.ErrAppendOnlyViolation) {
		return fmt.Sprintf("failed to prune %s: blocked by the append-only trigger's delete age floor; the configured retention is below the floor installed at boot — align retention config and restart the server to reinstall triggers: %v", what, err)
	}
	return fmt.Sprintf("failed to prune %s: %v", what, err)
}

// prune executes one pruning cycle.
func (p *AuditPruner) prune(ctx context.Context) {
	logger := slogging.Get()

	// Prune version snapshots first (they reference audit entries)
	snapshotsPruned, err := p.auditService.PruneVersionSnapshots(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("version snapshots", err))
	} else if snapshotsPruned > 0 {
		logger.Info("pruned %d version snapshots", snapshotsPruned)
	}

	// Sweep snapshots orphaned by hard-deleted entities (e.g. the threat-model
	// hard-delete cascade removes child rows but not their snapshots, #458).
	orphansPruned, err := p.auditService.PruneOrphanedVersionSnapshots(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("orphaned version snapshots", err))
	} else if orphansPruned > 0 {
		logger.Info("pruned %d orphaned version snapshots", orphansPruned)
	}

	// Then prune audit entries
	entriesPruned, err := p.auditService.PruneAuditEntries(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("audit entries", err))
	} else if entriesPruned > 0 {
		logger.Info("pruned %d audit entries", entriesPruned)
	}

	// Prune system audit entries (admin-write evidence, #400)
	systemPruned, err := p.auditService.PruneSystemAuditEntries(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("system audit entries", err))
	} else if systemPruned > 0 {
		logger.Info("pruned %d system audit entries", systemPruned)
	}

	// Purge expired tombstones (soft-deleted entities past retention period)
	tombstonesPurged, err := p.auditService.PurgeTombstones(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("expired tombstones", err))
	} else if tombstonesPurged > 0 {
		logger.Info("purged %d expired tombstones", tombstonesPurged)
	}
}
