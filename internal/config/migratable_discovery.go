package config

import (
	"reflect"
	"strings"
)

// DiscoveredField is a leaf-level field found by reflection over Config.
// It is the input the build-time completeness gate compares against the
// keys actually emitted by GetMigratableSettings — see
// TestMigratableSettings_CoverEveryConfigField in migratable_discovery_test.go.
type DiscoveredField struct {
	// DottedKey is the lower-snake-case dotted path produced from yaml
	// tags (e.g. "auth.cookie.enabled").
	DottedKey string

	// EnvVar is the value of the `env` struct tag on the leaf field, or
	// "" if absent. Used by the test to flag fields that should plumb a
	// TMI_* env var but don't.
	EnvVar string
}

// DiscoverConfigFields walks Config with reflection and returns every leaf
// field that should be a candidate for the migratable-settings emit set.
//
// Fields skipped (by design):
//   - Unexported fields (reflection cannot read them).
//   - Map types (auth.oauth.providers, auth.saml.providers,
//     content_oauth.providers): the per-instance key is dynamic, so
//     enumeration cannot produce a fixed dotted path. These are walked
//     by the per-provider helpers in migratable_settings.go.
//   - Slice-of-struct types (administrators): same reasoning — variable
//     cardinality, dynamic keys.
//   - Fields with yaml tag "-" (explicitly excluded).
//   - Anonymous (embedded) struct fields — none in Config today.
//
// The walker recurses into named struct fields. A leaf is anything that is
// NOT itself a struct (after dereferencing pointers).
func DiscoverConfigFields() []DiscoveredField {
	var out []DiscoveredField
	walkConfigType(reflect.TypeOf(Config{}), "", &out)
	return out
}

func walkConfigType(t reflect.Type, prefix string, out *[]DiscoveredField) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		yamlTag, ok := f.Tag.Lookup("yaml")
		if !ok {
			continue
		}
		name := strings.Split(yamlTag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		dotted := name
		if prefix != "" {
			dotted = prefix + "." + name
		}

		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		switch ft.Kind() {
		case reflect.Struct:
			// time.Duration is technically a typed int64 (Kind=Int64),
			// not a struct, so it does NOT land here. Real structs
			// recurse.
			walkConfigType(ft, dotted, out)
		case reflect.Map, reflect.Slice, reflect.Array:
			// Skip — dynamic cardinality. Provider maps and the
			// administrators slice are walked by the per-instance
			// helpers in migratable_settings.go. The completeness gate
			// does NOT enforce coverage here.
			continue
		default:
			*out = append(*out, DiscoveredField{
				DottedKey: dotted,
				EnvVar:    f.Tag.Get("env"),
			})
		}
	}
}

// ExpectedMigratableKeysSkipped returns the set of leaf fields that exist
// on Config but are intentionally NOT emitted as migratable settings. This
// allowlist is what keeps the completeness gate from flagging fields that
// have a legitimate reason to be invisible to the DB-backed settings
// service.
//
// Adding an entry here is a deliberate act — every entry must have a
// justification comment. An entry that says "TODO: migrate later" is a
// promise to do that work, tracked by the follow-up issue noted in the
// comment.
func ExpectedMigratableKeysSkipped() map[string]string {
	return map[string]string{ //nolint:gosec // G101 false positive — keys here are config field paths, not credentials
		// --- Bootstrap-by-construction ---

		// ContentTokenEncryptionKey is bootstrap-only: it's the key used
		// to encrypt content provider tokens at rest. Moving it to the
		// DB would mean the DB-decryption key lives in the DB it must
		// decrypt — a chicken-and-egg cycle.
		"content_token_encryption_key": "bootstrap encryption key for content provider tokens; chicken-and-egg if DB-backed",

		// Database.OracleWalletLocation is bootstrap-only: it's the
		// filesystem path to the Oracle wallet used at DB connect time,
		// well before SettingsService exists.
		"database.oracle_wallet_location": "filesystem path used at DB connect time; cannot be DB-backed by construction",

		// --- Renamed in the emit set ---

		// auth.oauth.callback_url is emitted as auth.oauth_callback_url
		// (note: underscore in the second-level segment, no dot). The
		// key rename is preserved for backward compatibility with
		// existing /admin/settings rows. Future cleanup: rename the
		// struct field's yaml tag to match, drop this skip entry.
		"auth.oauth.callback_url": "emitted as auth.oauth_callback_url (legacy key shape); rename later",

		// auth.saml.enabled is emitted as features.saml_enabled (the
		// public /config endpoint shape). The struct field is kept for
		// YAML compatibility; the emitted key matches the API contract.
		"auth.saml.enabled": "emitted as features.saml_enabled (public /config key shape)",

		// --- Tracked in #426: subsystems missing from migratable_settings.go ---
		// These are real gaps surfaced by #421's completeness gate.
		// Each emits no row today; the gate keeps them on a written
		// list rather than letting them silently miss migration.
		// Filing follow-up issue to plumb each block into the
		// per-subsystem helpers in migratable_settings.go.

		"content_extractors.compressed_size_bytes":        "TODO #426: plumb content_extractors.* into migratable_settings.go",
		"content_extractors.decompressed_size_bytes":      "TODO #426: plumb content_extractors.* into migratable_settings.go",
		"content_extractors.markdown_size_bytes":          "TODO #426: plumb content_extractors.* into migratable_settings.go",
		"content_extractors.part_size_bytes":              "TODO #426: plumb content_extractors.* into migratable_settings.go",
		"content_extractors.per_user_concurrency_default": "TODO #426: plumb content_extractors.* into migratable_settings.go",
		"content_extractors.pptx_slides":                  "TODO #426: plumb content_extractors.* into migratable_settings.go",
		"content_extractors.wall_clock_budget":            "TODO #426: plumb content_extractors.* into migratable_settings.go",
		"content_extractors.xlsx_cells":                   "TODO #426: plumb content_extractors.* into migratable_settings.go",

		"content_oauth.callback_url": "TODO #426: plumb content_oauth.* into migratable_settings.go",

		"content_sources.confluence.enabled":                    "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_drive.browser_oauth_client_id":  "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_drive.credentials_file":         "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_drive.enabled":                  "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_drive.picker_app_id":            "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_drive.picker_developer_key":     "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_drive.service_account_email":    "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_workspace.enabled":              "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_workspace.picker_app_id":        "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.google_workspace.picker_developer_key": "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.microsoft.application_object_id":       "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.microsoft.client_id":                   "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.microsoft.enabled":                     "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.microsoft.picker_origin":               "TODO #426: plumb content_sources.* into migratable_settings.go",
		"content_sources.microsoft.tenant_id":                   "TODO #426: plumb content_sources.* into migratable_settings.go",

		"observability.enabled":         "TODO #426: plumb observability.* into migratable_settings.go (OTEL config)",
		"observability.prometheus_port": "TODO #426: plumb observability.* into migratable_settings.go (OTEL config)",
		"observability.sampling_rate":   "TODO #426: plumb observability.* into migratable_settings.go (OTEL config)",

		"server.disable_rate_limiting": "TODO #426: plumb server.* rate-limit fields into migratable_settings.go",
		"server.ratelimit_public_rpm":  "TODO #426: plumb server.* rate-limit fields into migratable_settings.go",
		"server.require_if_match":      "TODO #426: plumb server.require_if_match into migratable_settings.go",

		"ssrf.document_uri.allowlist":   "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.document_uri.schemes":     "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.issue_uri.allowlist":      "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.issue_uri.schemes":        "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.repository_uri.allowlist": "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.repository_uri.schemes":   "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.timmy.allowlist":          "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.timmy.schemes":            "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.webhook.allowlist":        "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",
		"ssrf.webhook.schemes":          "TODO #426: plumb ssrf.* allowlists into migratable_settings.go",

		"webhooks.allow_http_targets": "TODO #426: plumb webhooks.* into migratable_settings.go",
	}
}
