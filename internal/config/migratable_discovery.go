package config

import (
	"reflect"
	"strings"
)

// DiscoveredField is a leaf-level field found by reflection over Config.
// It is the input the build-time completeness gate compares against the
// keys actually emitted by GetMigratableSettings — see
// TestMigratableSettings_CoverEveryConfigField in migratable_discovery_test.go.
// SEM@ac5e96c911b24411e0a2143b92aa3725fb7df190: dotted-key and env-var metadata for a single scalar config leaf field (pure)
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
// SEM@ac5e96c911b24411e0a2143b92aa3725fb7df190: list all scalar leaf fields in Config using yaml struct tags via reflection (pure)
func DiscoverConfigFields() []DiscoveredField {
	var out []DiscoveredField
	walkConfigType(reflect.TypeOf(Config{}), "", &out)
	return out
}

// SEM@ac5e96c911b24411e0a2143b92aa3725fb7df190: recursively collect scalar yaml-tagged config fields, skipping maps and slices (pure)
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
// SEM@d34da3918d4a3784077a74aedd722e45c29196cf: return the allowlist of config fields intentionally excluded from migratable settings, with justification (pure)
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

		// --- Renamed in the emit set (rename would break existing DB rows) ---

		// auth.oauth.callback_url is emitted as auth.oauth_callback_url
		// (note: underscore in the second-level segment, no dot). The
		// key rename is preserved for backward compatibility with
		// existing /admin/settings rows written before #426. Renaming the
		// DB key would silently drop the operator's stored value on next
		// SeedDefaults run. Future cleanup requires a DB migration.
		"auth.oauth.callback_url": "emitted as auth.oauth_callback_url (legacy key shape); rename requires DB migration",

		// auth.saml.enabled is emitted as features.saml_enabled (the
		// public /config endpoint shape). The struct field is kept for
		// YAML compatibility; the emitted key matches the API contract.
		// Renaming would break tmi-ux clients reading the /config endpoint.
		"auth.saml.enabled": "emitted as features.saml_enabled (public /config key shape); rename breaks API contract",

		// content_extractors.async_enabled is emitted as extraction.async_enabled
		// (#347). The backing field lives on ContentExtractorsConfig because it
		// governs the extraction pipeline, but the canonical settings key uses the
		// logical "extraction.*" namespace to allow future extraction-related
		// operational settings to live alongside it without polluting the
		// content_extractors.* size/limit namespace.
		"content_extractors.async_enabled": "emitted as extraction.async_enabled (#347); key namespace matches extraction pipeline domain",
	}
}
