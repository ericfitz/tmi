package api

import (
	"context"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// AccessPoller periodically checks documents with "pending_access" status.
// SEM@d994c2f113f9e0997f83a0815018638cc94111f7: background service that periodically checks pending-access documents and transitions them to accessible (mutates shared state)
type AccessPoller struct {
	sources       *ContentSourceRegistry
	documentStore DocumentRepository
	pipeline      *ContentPipeline      // optional; when set, attempts extraction on accessible transition
	linkedChecker LinkedProviderChecker // optional; when nil, picker-aware dispatch falls back to URL-based
	// Async extraction collaborators (Task 8 / Plan 3 of #347). Both must be
	// non-nil and asyncDecider must return true for the async path to activate.
	publisher    *ExtractionPublisher
	asyncDecider func(ctx context.Context) bool
	interval     time.Duration
	maxAge       time.Duration
	stopCh       chan struct{}
	stopOnce     sync.Once // ensures Stop is idempotent (no double-close panic)
}

// NewAccessPoller creates a new background access poller.
// SEM@f7d829c2058f4f0be9f76648be2cbcfc3501f485: build an AccessPoller with a polling interval and max document age (pure)
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
// SEM@d330121ff53e262b1d2c0ff6713294e41f615330: inject a linked provider checker for picker-aware document dispatch (mutates shared state)
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
// SEM@a3a8b3e82371d176bbcdfb7444b4ac361b0f8ace: inject a content pipeline to attempt extraction on document access transition (mutates shared state)
func (p *AccessPoller) SetContentPipeline(pipeline *ContentPipeline) {
	p.pipeline = pipeline
}

// SetAsyncExtraction injects the async extraction publisher and decider into
// the poller. When the decider returns true AND publisher is non-nil, pollOnce
// routes extraction through the worker pipeline (fetch bytes + publish job)
// instead of running inline extraction. The document is left in pending_access;
// the result-consumer transitions it to accessible when the worker completes.
//
// Pass nil publisher or a nil decider to keep the inline path.
// Lifecycle: must be called BEFORE Start.
// SEM@d994c2f113f9e0997f83a0815018638cc94111f7: inject an async extraction publisher and decider to route polling through the worker pipeline (mutates shared state)
func (p *AccessPoller) SetAsyncExtraction(publisher *ExtractionPublisher, decider func(context.Context) bool) {
	p.publisher = publisher
	p.asyncDecider = decider
}

// Start begins the background polling loop.
// SEM@7f13559c1b4f9930b12898ca3e23b47987cae72c: launch the background polling goroutine (mutates shared state)
func (p *AccessPoller) Start() {
	go p.run()
}

// Stop signals the poller to stop. Safe to call more than once; subsequent
// calls are no-ops. The goroutine exits asynchronously; callers that need
// synchronous teardown should wait on a done channel (not exposed here
// because the common pattern is fire-and-forget from the holder).
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: signal the polling goroutine to stop; idempotent (mutates shared state)
func (p *AccessPoller) Stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
}

// SEM@8c931151a5e0874ff74b933545bdb5443a763565: ticker loop that calls pollOnce on each interval until stopped (mutates shared state)
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

// SEM@d994c2f113f9e0997f83a0815018638cc94111f7: check each pending-access document for accessibility and trigger inline or async extraction (reads DB)
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

			// Async extraction path (Plan 3 of #347): when the flag is on AND a
			// publisher is wired, fetch raw bytes and submit to the worker pipeline.
			// The document stays in pending_access; the result-consumer transitions
			// it to accessible once the worker completes. On any fetch/publish error
			// we fall through to the inline path so extraction is never silently
			// dropped.
			if p.pipeline != nil && p.publisher != nil && p.asyncDecider != nil && p.asyncDecider(ctx) {
				data, contentType, fetchErr := p.pipeline.FetchForPublish(ctx, doc.Uri)
				if fetchErr == nil {
					if jobID, pubErr := p.publisher.Publish(ctx, ExtractionRequest{
						DocumentID:  doc.Id.String(),
						ContentType: contentType,
						Bytes:       data,
					}); pubErr == nil {
						logger.Info("AccessPoller: async extraction job %s published for document %s", jobID, doc.Id)
						continue // leave status pending_access; result-consumer finishes it
					} else {
						logger.Warn("AccessPoller: async publish failed for %s, falling back to inline: %v", doc.Id, pubErr)
					}
				} else {
					logger.Warn("AccessPoller: fetch-for-publish failed for %s, falling back to inline: %v", doc.Id, fetchErr)
				}
			}

			// Inline extraction path: if a content pipeline is wired, attempt
			// extraction so that any failure (limit tripped, malformed input,
			// timeout) is classified and persisted as extraction_failed with a
			// stable reason code. On success, clear any prior diagnostic by
			// writing accessible.
			//
			// pollOnce intentionally calls Extract with context.Background()
			// (no user ID). The pipeline's per-user concurrency limiter is
			// therefore bypassed, which is correct: the poller is
			// single-threaded and processes documents sequentially, so
			// there is no concurrent extraction to gate. If this code is
			// ever parallelized for throughput, a "system" user ID (or a
			// separate system-wide limiter) should be introduced to keep
			// the cap honest.
			if p.pipeline != nil {
				if _, extErr := p.pipeline.ExtractForDocument(ctx, doc); extErr != nil {
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
