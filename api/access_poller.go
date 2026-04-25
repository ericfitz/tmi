package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// AccessPoller periodically checks documents with "pending_access" status.
type AccessPoller struct {
	sources       *ContentSourceRegistry
	documentStore DocumentStore
	linkedChecker LinkedProviderChecker // optional; when nil, picker-aware dispatch falls back to URL-based
	interval      time.Duration
	maxAge        time.Duration
	stopCh        chan struct{}
}

// NewAccessPoller creates a new background access poller.
func NewAccessPoller(
	sources *ContentSourceRegistry,
	documentStore DocumentStore,
	interval time.Duration,
	maxAge time.Duration,
) *AccessPoller {
	return &AccessPoller{
		sources:       sources,
		documentStore: documentStore,
		interval:      interval,
		maxAge:        maxAge,
		stopCh:        make(chan struct{}),
	}
}

// SetLinkedProviderChecker injects a LinkedProviderChecker so the poller
// can dispatch picker-attached documents to their delegated source via
// FindSourceForDocument. Optional — when omitted, the poller behaves as
// before (URL-based dispatch only).
func (p *AccessPoller) SetLinkedProviderChecker(c LinkedProviderChecker) {
	p.linkedChecker = c
}

// Start begins the background polling loop.
func (p *AccessPoller) Start() {
	go p.run()
}

// Stop signals the poller to stop.
func (p *AccessPoller) Stop() {
	close(p.stopCh)
}

func (p *AccessPoller) run() {
	logger := slogging.Get()
	logger.Info("AccessPoller: started (interval=%s, maxAge=%s)", p.interval, p.maxAge)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			logger.Info("AccessPoller: stopped")
			return
		case <-ticker.C:
			p.pollOnce()
		}
	}
}

func (p *AccessPoller) pollOnce() {
	logger := slogging.Get()
	ctx := context.Background()

	if p.documentStore == nil {
		return
	}

	docs, err := p.documentStore.ListByAccessStatus(ctx, AccessStatusPendingAccess, 100)
	if err != nil {
		logger.Warn("AccessPoller: failed to list pending documents: %v", err)
		return
	}

	if len(docs) == 0 {
		return
	}

	logger.Debug("AccessPoller: checking %d pending documents", len(docs))

	for _, doc := range docs {
		// Skip documents older than maxAge
		if doc.CreatedAt != nil && time.Since(*doc.CreatedAt) > p.maxAge {
			logger.Debug("AccessPoller: skipping expired document %s (created %s)", doc.Id, doc.CreatedAt)
			continue
		}

		// Load picker metadata + owner for picker-aware dispatch.
		picker, ownerUUID, dispatchErr := p.documentStore.GetPickerDispatch(ctx, doc.Id.String())
		if dispatchErr != nil {
			logger.Warn("AccessPoller: failed to load picker dispatch for %s: %v", doc.Id, dispatchErr)
			// Fall through to URL-based dispatch with no picker context.
			picker = nil
			ownerUUID = ""
		}

		src, ok := p.sources.FindSourceForDocument(ctx, doc.Uri, picker, ownerUUID, p.linkedChecker)
		if !ok {
			continue
		}

		validator, ok := src.(AccessValidator)
		if !ok {
			continue
		}

		accessible, valErr := validator.ValidateAccess(ctx, doc.Uri)
		if valErr != nil {
			logger.Debug("AccessPoller: validation error for %s: %v", doc.Uri, valErr)
			continue
		}

		if accessible {
			logger.Info("AccessPoller: document %s is now accessible", doc.Id)
			if updateErr := p.documentStore.UpdateAccessStatus(ctx, doc.Id.String(), AccessStatusAccessible, ""); updateErr != nil {
				logger.Warn("AccessPoller: failed to update document %s: %v", doc.Id, updateErr)
			}
		}
	}
}
