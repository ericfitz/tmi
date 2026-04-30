package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// AccessPoller periodically checks documents with "pending_access" status.
type AccessPoller struct {
	sources       *ContentSourceRegistry
	documentStore DocumentRepository
	pipeline      *ContentPipeline      // optional; when set, attempts extraction on accessible transition
	linkedChecker LinkedProviderChecker // optional; when nil, picker-aware dispatch falls back to URL-based
	interval      time.Duration
	maxAge        time.Duration
	stopCh        chan struct{}
}

// NewAccessPoller creates a new background access poller.
func NewAccessPoller(
	sources *ContentSourceRegistry,
	documentStore DocumentRepository,
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
//
// Lifecycle: must be called BEFORE Start. Calling SetLinkedProviderChecker
// after Start races with the poll goroutine reading the field; in
// production wiring (cmd/server/main.go) the checker is configured
// during init alongside the rest of the poller setup.
func (p *AccessPoller) SetLinkedProviderChecker(c LinkedProviderChecker) {
	p.linkedChecker = c
}

// SetContentPipeline injects a content pipeline so the poller can attempt
// extraction once a document transitions to accessible. When extraction
// fails, the poller classifies the failure and persists the access_status
// + reason_code via UpdateAccessStatusWithDiagnostics. Optional — when
// omitted, the poller updates to AccessStatusAccessible without attempting
// extraction (legacy behavior).
//
// Lifecycle: same as SetLinkedProviderChecker; must be called BEFORE Start.
func (p *AccessPoller) SetContentPipeline(pipeline *ContentPipeline) {
	p.pipeline = pipeline
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
			logger.Warn("AccessPoller: GetPickerDispatch failed for doc %s (uri=%s); falling back to URL-based dispatch: %v", doc.Id, doc.Uri, dispatchErr)
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
			// If a content pipeline is wired, attempt extraction so that any
			// failure (limit tripped, malformed input, timeout) is classified
			// and persisted as extraction_failed with a stable reason code.
			// On success, clear any prior diagnostic by writing accessible.
			if p.pipeline != nil {
				if _, extErr := p.pipeline.Extract(ctx, doc.Uri); extErr != nil {
					classified := ClassifyExtractionError(extErr)
					contentSource := src.Name()
					logger.Warn("AccessPoller: extraction failed for %s (%s): %v",
						doc.Id, classified.ReasonCode, extErr)
					if updateErr := p.documentStore.UpdateAccessStatusWithDiagnostics(
						ctx, doc.Id.String(), classified.Status, contentSource, classified.ReasonCode, classified.ReasonDetail,
					); updateErr != nil {
						logger.Warn("AccessPoller: failed to update document %s after extraction failure: %v", doc.Id, updateErr)
					}
					continue
				}
			}
			// Clear any prior diagnostic when transitioning to accessible.
			if updateErr := p.documentStore.UpdateAccessStatusWithDiagnostics(ctx, doc.Id.String(), AccessStatusAccessible, "", "", ""); updateErr != nil {
				logger.Warn("AccessPoller: failed to update document %s: %v", doc.Id, updateErr)
			}
		}
	}
}
