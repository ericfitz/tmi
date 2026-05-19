package config

import "strings"

// classificationFor returns the ConfigClass for a setting key. It checks the
// exact-match table first, then the ordered prefix table. An unknown key
// returns the zero ConfigClass (CategoryUnclassified), which the validation
// suite rejects.
func classificationFor(key string) ConfigClass {
	if c, ok := exactClassifications[key]; ok {
		return c
	}
	for _, p := range prefixClassifications {
		if strings.HasPrefix(key, p.prefix) {
			return p.class
		}
	}
	return ConfigClass{}
}

type prefixClass struct {
	prefix string
	class  ConfigClass
}

// bootstrapClass is a helper for the common bootstrap shape.
func bootstrapClass(required bool, vis Visibility, secret bool) ConfigClass {
	return ConfigClass{
		Category:   CategoryBootstrap,
		Secret:     secret,
		ValueKind:  ValueKindInline,
		Visibility: vis,
		Mutability: MutabilityStatic,
		Consumers:  []Consumer{ConsumerMonolith},
		Required:   required,
	}
}

// operationalClass is a helper for the common monolith-only operational shape.
func operationalClass(vis Visibility, secret bool, consumers ...Consumer) ConfigClass {
	if len(consumers) == 0 {
		consumers = []Consumer{ConsumerMonolith}
	}
	return ConfigClass{
		Category:   CategoryOperational,
		Secret:     secret,
		ValueKind:  ValueKindInline,
		Delivery:   &Delivery{},
		Visibility: vis,
		Mutability: MutabilityHot,
		Consumers:  consumers,
	}
}

// sharedEmbeddingClass is the SharedInvariant shape for the embedding profile.
func sharedEmbeddingClass(secret bool) ConfigClass {
	return ConfigClass{
		Category:   CategoryOperational,
		Secret:     secret,
		ValueKind:  ValueKindInline,
		Delivery:   &Delivery{StampedIntoEnvelope: true, SharedInvariant: true},
		Visibility: VisibilityAdminOnly,
		Mutability: MutabilityHot,
		Consumers:  []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed},
	}
}

// exactClassifications maps exact setting keys to their ConfigClass.
// Every key produced by GetMigratableSettings must appear here or match a
// prefix in prefixClassifications, or the validation suite fails.
var exactClassifications = map[string]ConfigClass{
	// --- Bootstrap: server ---
	"server.port":                   bootstrapClass(true, VisibilityInternal, false),
	"server.interface":              bootstrapClass(true, VisibilityInternal, false),
	"server.tls_enabled":            bootstrapClass(false, VisibilityInternal, false),
	"server.tls_subject_name":       bootstrapClass(false, VisibilityInternal, false),
	"server.tls_cert_file":          bootstrapClass(false, VisibilityInternal, false),
	"server.tls_key_file":           bootstrapClass(false, VisibilityInternal, true),
	"server.http_to_https_redirect": bootstrapClass(false, VisibilityInternal, false),
	"server.read_timeout":           bootstrapClass(false, VisibilityInternal, false),
	"server.write_timeout":          bootstrapClass(false, VisibilityInternal, false),
	"server.idle_timeout":           bootstrapClass(false, VisibilityInternal, false),
	"server.base_url":               bootstrapClass(false, VisibilityPublic, false),
	"server.cors.allowed_origins":   bootstrapClass(false, VisibilityInternal, false),

	// --- Bootstrap: database ---
	"database.url": bootstrapClass(true, VisibilityInternal, true),
	"database.connection_pool.max_open_conns":     bootstrapClass(false, VisibilityInternal, false),
	"database.connection_pool.max_idle_conns":     bootstrapClass(false, VisibilityInternal, false),
	"database.connection_pool.conn_max_lifetime":  bootstrapClass(false, VisibilityInternal, false),
	"database.connection_pool.conn_max_idle_time": bootstrapClass(false, VisibilityInternal, false),
	"database.redis.url":                          bootstrapClass(false, VisibilityInternal, true),
	"database.redis.host":                         bootstrapClass(false, VisibilityInternal, false),
	"database.redis.port":                         bootstrapClass(false, VisibilityInternal, false),
	"database.redis.password":                     bootstrapClass(false, VisibilityInternal, true),
	"database.redis.db":                           bootstrapClass(false, VisibilityInternal, false),

	// --- Bootstrap: auth (JWT signing, build mode) ---
	"auth.build_mode":         bootstrapClass(true, VisibilityInternal, false),
	"auth.jwt.secret":         bootstrapClass(true, VisibilityInternal, true),
	"auth.jwt.signing_method": bootstrapClass(false, VisibilityInternal, false),

	// --- Operational: auth (runtime-tunable auth knobs) ---
	"auth.auto_promote_first_user":   operationalClass(VisibilityAdminOnly, false),
	"auth.everyone_is_a_reviewer":    operationalClass(VisibilityAdminOnly, false),
	"auth.jwt.expiration_seconds":    operationalClass(VisibilityAdminOnly, false),
	"auth.jwt.refresh_token_days":    operationalClass(VisibilityAdminOnly, false),
	"auth.jwt.session_lifetime_days": operationalClass(VisibilityAdminOnly, false),
	"auth.step_up_window_seconds":    operationalClass(VisibilityAdminOnly, false),
	"auth.cookie.enabled":            operationalClass(VisibilityAdminOnly, false),
	"auth.cookie.domain":             operationalClass(VisibilityAdminOnly, false),
	"auth.cookie.secure":             operationalClass(VisibilityAdminOnly, false),
	"auth.oauth_callback_url":        operationalClass(VisibilityAdminOnly, false),

	// --- Operational: feature flags, runtime, operator ---
	"features.saml_enabled":                operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX),
	"websocket.inactivity_timeout_seconds": operationalClass(VisibilityAdminOnly, false),
	"session.timeout_minutes":              operationalClass(VisibilityAdminOnly, false),
	"operator.name":                        operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX),
	"operator.contact":                     operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX),
	"administrators":                       operationalClass(VisibilityAdminOnly, false),

	// --- Bootstrap: logging & observability ---
	"logging.level":                         bootstrapClass(false, VisibilityInternal, false),
	"logging.is_dev":                        bootstrapClass(false, VisibilityInternal, false),
	"logging.is_test":                       bootstrapClass(false, VisibilityInternal, false),
	"logging.log_dir":                       bootstrapClass(false, VisibilityInternal, false),
	"logging.max_age_days":                  bootstrapClass(false, VisibilityInternal, false),
	"logging.max_size_mb":                   bootstrapClass(false, VisibilityInternal, false),
	"logging.max_backups":                   bootstrapClass(false, VisibilityInternal, false),
	"logging.also_log_to_console":           bootstrapClass(false, VisibilityInternal, false),
	"logging.cloud_error_threshold":         bootstrapClass(false, VisibilityInternal, false),
	"logging.log_api_requests":              bootstrapClass(false, VisibilityInternal, false),
	"logging.log_api_responses":             bootstrapClass(false, VisibilityInternal, false),
	"logging.log_websocket_messages":        bootstrapClass(false, VisibilityInternal, false),
	"logging.redact_auth_tokens":            bootstrapClass(false, VisibilityInternal, false),
	"logging.suppress_unauthenticated_logs": bootstrapClass(false, VisibilityInternal, false),

	// --- Bootstrap: secrets provider ---
	"secrets.provider":           bootstrapClass(false, VisibilityInternal, false),
	"secrets.vault_address":      bootstrapClass(false, VisibilityInternal, false),
	"secrets.vault_path":         bootstrapClass(false, VisibilityInternal, false),
	"secrets.vault_token":        bootstrapClass(false, VisibilityInternal, true),
	"secrets.aws_region":         bootstrapClass(false, VisibilityInternal, false),
	"secrets.aws_secret_name":    bootstrapClass(false, VisibilityInternal, false),
	"secrets.azure_vault_url":    bootstrapClass(false, VisibilityInternal, false),
	"secrets.gcp_project_id":     bootstrapClass(false, VisibilityInternal, false),
	"secrets.gcp_secret_name":    bootstrapClass(false, VisibilityInternal, false),
	"secrets.oci_compartment_id": bootstrapClass(false, VisibilityInternal, false),
	"secrets.oci_vault_id":       bootstrapClass(false, VisibilityInternal, false),
	"secrets.oci_secret_name":    bootstrapClass(false, VisibilityInternal, false),

	// --- Shared: embedding profile (text) ---
	"timmy.text_embedding_model":    sharedEmbeddingClass(false),
	"timmy.text_embedding_base_url": sharedEmbeddingClass(false),
	// The embedding API key is a secret; it is NOT stamped into the envelope —
	// it is resolved from a mounted secret. Classified bootstrap.
	"timmy.text_embedding_api_key": bootstrapClass(false, VisibilityInternal, true),
}

// prefixClassifications handles repeating provider keys
// (auth.oauth.providers.*, auth.saml.providers.*, content_oauth.providers.*).
// The list is ordered; the first matching prefix wins.
var prefixClassifications = []prefixClass{
	{
		prefix: "auth.oauth.providers.",
		class:  operationalClass(VisibilityAdminOnly, true),
	},
	{
		prefix: "auth.saml.providers.",
		class:  operationalClass(VisibilityAdminOnly, true),
	},
	{
		prefix: "content_oauth.providers.",
		class:  operationalClass(VisibilityAdminOnly, true),
	},
	{
		prefix: "content_extractors.",
		class:  operationalClass(VisibilityAdminOnly, false),
	},
	{
		prefix: "content_sources.",
		class:  operationalClass(VisibilityAdminOnly, false),
	},
	{
		prefix: "timmy.",
		// All remaining timmy.* keys (chunk size, top-k, timeouts) are
		// operational, monolith-relayed to the chunk-embed worker.
		class: ConfigClass{
			Category:   CategoryOperational,
			ValueKind:  ValueKindInline,
			Delivery:   &Delivery{StampedIntoEnvelope: true},
			Visibility: VisibilityAdminOnly,
			Mutability: MutabilityHot,
			Consumers:  []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed},
		},
	},
}
