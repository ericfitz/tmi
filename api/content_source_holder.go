package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// ContentSourceBundle groups the rebuildable content-source objects served to
// request handlers and background pollers. It is immutable once built;
// ContentSourceHolder swaps the whole struct on rebuild.
type ContentSourceBundle struct {
	Sources  *ContentSourceRegistry
	Pipeline *ContentPipeline
	Poller   *AccessPoller
}

// ContentSourceBundleBuilder constructs a ContentSourceBundle from a resolved
// config. Injected so tests can substitute a fake builder instead of
// constructing real OAuth clients or filesystem sources.
//
// Implementations MUST NOT call Start() on the returned Poller — the holder
// calls Start() after installing the bundle so the poller always runs against
// the live sources.
//
// On misconfiguration the builder should return (bundle, nil) with only the
// healthy sources registered (logging a WARN for each skipped source), NOT
// return an error that would leave the server with no sources at all. A nil
// bundle is returned only when the entire subsystem cannot be set up (e.g.
// nil document store), in which case the holder keeps its previous bundle.
type ContentSourceBundleBuilder func(ctx context.Context, cfg config.Config) (*ContentSourceBundle, error)

// ContentSourceHolder owns the live ContentSourceBundle and rebuilds it lazily
// when the wiring configuration has changed. Safe for concurrent use.
//
// The pattern mirrors TimmyCore: a stable hash of the config fields that
// affect source construction gates whether a rebuild is needed. In-flight
// requests hold their *ContentSourceBundle pointer for the duration of the
// call; a swap replaces the holder's pointer but never mutates an in-use
// bundle.
type ContentSourceHolder struct {
	cfg   func(ctx context.Context) config.Config // live config reader
	build ContentSourceBundleBuilder

	mu        sync.RWMutex
	current   *ContentSourceBundle
	hash      string
	onRebuild []func(*ContentSourceBundle) // callbacks invoked after each successful rebuild (under WLock)
}

// NewContentSourceHolder constructs a holder over the given config reader and
// builder. Neither argument may be nil.
func NewContentSourceHolder(
	cfgReader func(ctx context.Context) config.Config,
	builder ContentSourceBundleBuilder,
) *ContentSourceHolder {
	return &ContentSourceHolder{cfg: cfgReader, build: builder}
}

// AddRebuildHook registers a callback that is invoked (while the write lock is
// held) after each successful rebuild. Hooks are called in registration order
// with the newly installed bundle. The bundle is never nil when a hook fires.
// Hooks MUST NOT call Get (deadlock) or block for more than a few microseconds.
func (h *ContentSourceHolder) AddRebuildHook(fn func(*ContentSourceBundle)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onRebuild = append(h.onRebuild, fn)
}

// Get returns the live ContentSourceBundle, rebuilding it if the wiring
// configuration has changed since the last build. A build error is returned
// to the caller and does NOT poison the cache: the next Get retries. Callers
// hold the returned pointer for the duration of their request; a concurrent
// rebuild swaps the holder's pointer but never mutates an in-use bundle.
//
// Concurrency: the fast path (hash unchanged) acquires only an RLock.
// A rebuild upgrades to WLock with a double-check to prevent two goroutines
// from both rebuilding.
func (h *ContentSourceHolder) Get(ctx context.Context) (*ContentSourceBundle, error) {
	cfg := h.cfg(ctx)
	want := contentSourceWiringHash(cfg)

	h.mu.RLock()
	if h.current != nil && h.hash == want {
		b := h.current
		h.mu.RUnlock()
		return b, nil
	}
	h.mu.RUnlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	// Double-check: another goroutine may have rebuilt while we waited for WLock.
	if h.current != nil && h.hash == want {
		return h.current, nil
	}
	bundle, err := h.build(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Stop the old poller before installing the new bundle so its ticker goroutine
	// is released. The new poller is started after the swap.
	if h.current != nil && h.current.Poller != nil {
		h.current.Poller.Stop()
	}
	h.current = bundle
	h.hash = want
	if bundle != nil && bundle.Poller != nil {
		bundle.Poller.Start()
	}
	// Notify rebuild hooks (e.g. to push the new pipeline into the document handler).
	if bundle != nil {
		for _, fn := range h.onRebuild {
			fn(bundle)
		}
	}
	return h.current, nil
}

// StopPoller stops the currently-running access poller, if any. Called during
// server shutdown so the goroutine is cleaned up before the process exits.
func (h *ContentSourceHolder) StopPoller() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.current != nil && h.current.Poller != nil {
		h.current.Poller.Stop()
	}
}

// contentSourceWiringHash returns a stable hash over the config fields that,
// when changed, require rebuilding content sources. This mirrors the
// TimmyConfigProvider.WiringHash pattern.
//
// Fields included: every sub-config that affects which sources are constructed
// and how they connect to external providers (OAuth credentials, service-account
// paths, picker keys, encryption key, allowlists). Pure tuning values (e.g.
// access-poller interval) are NOT included because they are read live inside the
// poller.
func contentSourceWiringHash(cfg config.Config) string {
	cs := cfg.ContentSources
	co := cfg.ContentOAuth

	// Flatten provider configs deterministically: sort by ID, then join key fields.
	providerIDs := make([]string, 0, len(co.Providers))
	for id := range co.Providers {
		providerIDs = append(providerIDs, id)
	}
	// Simple deterministic sort without importing "sort" package.
	for i := 0; i < len(providerIDs); i++ {
		for j := i + 1; j < len(providerIDs); j++ {
			if providerIDs[i] > providerIDs[j] {
				providerIDs[i], providerIDs[j] = providerIDs[j], providerIDs[i]
			}
		}
	}
	var providerParts []string
	for _, id := range providerIDs {
		p := co.Providers[id]
		providerParts = append(providerParts,
			id,
			boolStr(p.Enabled),
			p.ClientID,
			p.ClientSecret,
			p.AuthURL,
			p.TokenURL,
			strings.Join(p.RequiredScopes, ","),
		)
	}

	fields := []string{
		// timmy.enabled gates the whole subsystem; disabling it tears down the
		// access poller and clears the registry.
		boolStr(cfg.Timmy.Enabled),

		// Content token encryption key — changing it means sources re-init with new encryptor.
		cfg.ContentTokenEncryptionKey,

		// Google Drive service-account source
		boolStr(cs.GoogleDrive.Enabled),
		cs.GoogleDrive.CredentialsFile,
		cs.GoogleDrive.ServiceAccountEmail,
		cs.GoogleDrive.PickerDeveloperKey,
		cs.GoogleDrive.PickerAppID,

		// Google Workspace delegated source
		boolStr(cs.GoogleWorkspace.Enabled),
		cs.GoogleWorkspace.PickerDeveloperKey,
		cs.GoogleWorkspace.PickerAppID,

		// Confluence delegated source
		boolStr(cs.Confluence.Enabled),

		// Microsoft delegated source
		boolStr(cs.Microsoft.Enabled),
		cs.Microsoft.TenantID,
		cs.Microsoft.ClientID,
		cs.Microsoft.ApplicationObjectID,
		cs.Microsoft.PickerOrigin,

		// OAuth providers (all of them, since they are referenced by source builders).
		strings.Join(providerParts, "\x01"),
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(sum[:])
}

// boolStr converts a bool to a stable string for hashing.
func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// ContentPipelineFactory builds a ContentPipeline from a given
// ContentSourceRegistry. The factory captures the startup-wired extractor
// registry, concurrency limiter, and limits so they can be reused across
// registry rebuilds. Only the source registry changes on runtime toggle.
type ContentPipelineFactory func(sources *ContentSourceRegistry) *ContentPipeline

// BuildContentSourceBundle is the production builder for ContentSourceHolder.
// It mirrors the logic that was in initializeTimmySubsystem but converts every
// os.Exit(1) content-source validation into a graceful WARN+skip so a live
// server is never crashed by a misconfigured content source.
//
// pipelineFactory is called with the newly-built source registry to assemble
// a ContentPipeline that wraps it. Pass nil to skip pipeline construction
// (pipeline will be nil in the returned bundle).
//
// The function signature matches ContentSourceBundleBuilder.
func BuildContentSourceBundle(
	tokenRepo ContentTokenRepository,
	contentOAuthValidator *URIValidator,
	timmyURIValidator *URIValidator,
	pipelineFactory ContentPipelineFactory,
) ContentSourceBundleBuilder {
	return func(ctx context.Context, cfg config.Config) (*ContentSourceBundle, error) {
		logger := slogging.Get()
		contentSources := NewContentSourceRegistry()

		// Rebuild ContentOAuthProviderRegistry from current config so newly
		// enabled/disabled OAuth providers are reflected without a restart.
		var oauthRegistry *ContentOAuthProviderRegistry
		if tokenRepo != nil && cfg.ContentTokenEncryptionKey != "" {
			var regErr error
			oauthRegistry, regErr = LoadContentOAuthRegistryFromConfig(cfg.ContentOAuth, contentOAuthValidator)
			if regErr != nil {
				logger.Warn("ContentSourceHolder: failed to load content OAuth registry, delegated sources disabled: %v", regErr)
				oauthRegistry = nil
			}
		}

		// ── Google Workspace delegated source ────────────────────────────────
		if cfg.ContentSources.GoogleWorkspace.Enabled {
			if tokenRepo == nil || oauthRegistry == nil {
				logger.Warn("content source google_workspace disabled: requires content-token encryption key and OAuth provider configuration")
			} else if _, ok := oauthRegistry.Get(ProviderGoogleWorkspace); !ok {
				logger.Warn("content source google_workspace disabled: requires content_oauth.providers.google_workspace.enabled=true")
			} else if !cfg.ContentSources.GoogleWorkspace.IsConfigured() {
				logger.Warn("content source google_workspace disabled: requires picker_developer_key and picker_app_id")
			} else {
				gwSource := NewDelegatedGoogleWorkspaceSource(
					tokenRepo,
					oauthRegistry,
					cfg.ContentSources.GoogleWorkspace.PickerDeveloperKey,
					cfg.ContentSources.GoogleWorkspace.PickerAppID,
				)
				contentSources.Register(gwSource)
				logger.Info("Content source enabled: google_workspace (delegated, drive.file scope)")
			}
		}

		// ── Confluence delegated source ──────────────────────────────────────
		if cfg.ContentSources.Confluence.Enabled {
			if tokenRepo == nil || oauthRegistry == nil {
				logger.Warn("content source confluence disabled: requires content-token encryption key and OAuth provider configuration")
			} else if confluenceProvider, ok := oauthRegistry.Get(ProviderConfluence); !ok {
				logger.Warn("content source confluence disabled: requires content_oauth.providers.confluence.enabled=true")
			} else {
				// Warn (non-fatal) when offline_access is missing.
				hasOfflineAccess := false
				for _, scope := range confluenceProvider.RequiredScopes() {
					if scope == "offline_access" {
						hasOfflineAccess = true
						break
					}
				}
				if !hasOfflineAccess {
					logger.Warn("content_oauth.providers.confluence.required_scopes does not include 'offline_access'; refresh tokens will not be issued and users will need to re-link after access tokens expire")
				}
				confluenceSource := NewDelegatedConfluenceSource(tokenRepo, oauthRegistry, timmyURIValidator)
				contentSources.Register(confluenceSource)
				logger.Info("Content source enabled: confluence (delegated)")
			}
		}

		// ── Microsoft delegated source ────────────────────────────────────────
		if cfg.ContentSources.Microsoft.Enabled {
			if tokenRepo == nil || oauthRegistry == nil {
				logger.Warn("content source microsoft disabled: requires content-token encryption key and OAuth provider configuration")
			} else if !cfg.ContentSources.Microsoft.IsConfigured() {
				logger.Warn("content source microsoft disabled: requires tenant_id, client_id, and application_object_id")
			} else if msProvider, ok := oauthRegistry.Get(ProviderMicrosoft); !ok {
				logger.Warn("content source microsoft disabled: requires content_oauth.providers.microsoft.enabled=true")
			} else {
				// Warn (non-fatal) when offline_access is missing.
				hasOfflineAccess := false
				for _, scope := range msProvider.RequiredScopes() {
					if scope == "offline_access" {
						hasOfflineAccess = true
						break
					}
				}
				if !hasOfflineAccess {
					logger.Warn("content_oauth.providers.microsoft.required_scopes does not include 'offline_access'; users will need to re-link after access tokens expire")
				}
				msSource := NewDelegatedMicrosoftSource(tokenRepo, oauthRegistry, timmyURIValidator)
				contentSources.Register(msSource)
				logger.Info("Content source enabled: microsoft (delegated, OneDrive-for-Business + SharePoint)")
			}
		}

		// ── Google Drive service-account source ──────────────────────────────
		if cfg.ContentSources.GoogleDrive.IsConfigured() {
			gdSource, gdErr := NewGoogleDriveSource(
				cfg.ContentSources.GoogleDrive.CredentialsFile,
				cfg.ContentSources.GoogleDrive.ServiceAccountEmail,
			)
			if gdErr != nil {
				logger.Warn("content source google_drive disabled: failed to initialize: %v", gdErr)
			} else {
				contentSources.Register(gdSource)
				logger.Info("Content source enabled: google_drive (service account: %s)",
					cfg.ContentSources.GoogleDrive.ServiceAccountEmail)
			}
		}

		// HTTP source is always last (catch-all for http/https URIs).
		contentSources.Register(NewHTTPSource(timmyURIValidator))

		logger.Info("ContentSourceHolder: content sources enabled: %s", strings.Join(contentSources.Names(), ", "))

		// Build the content pipeline from the (newly built) source registry if a
		// factory was provided. The factory reuses the startup-wired extractor
		// registry, limiter, and limits, replacing only the source registry.
		var pipeline *ContentPipeline
		if pipelineFactory != nil {
			pipeline = pipelineFactory(contentSources)
		}

		// AccessPoller: do NOT call Start() here — the holder calls Start() after
		// installing the bundle so the poller always runs against the live sources.
		poller := NewAccessPoller(
			contentSources,
			GlobalDocumentRepository,
			5*time.Minute,
			7*24*time.Hour,
		)
		if tokenRepo != nil {
			poller.SetLinkedProviderChecker(NewContentTokenLinkedChecker(tokenRepo))
		}

		return &ContentSourceBundle{
			Sources:  contentSources,
			Pipeline: pipeline,
			Poller:   poller,
		}, nil
	}
}
