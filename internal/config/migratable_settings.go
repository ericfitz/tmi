package config

import (
	"encoding/json"
	"os"
	"strconv"
)

// MigratableSetting represents a setting that can be migrated from config to database
// SEM@10b74985ed52c143cb0fb6e853b2d5f106de198f: DTO representing a single config setting eligible for database migration (pure)
type MigratableSetting struct {
	Key         string
	Value       string
	Type        string
	Description string
	Secret      bool   // true = mask value in API responses (kept for back-compat; mirrors Class.Secret)
	Source      string // "config" or "environment"
	EnvVar      string // TMI_* environment variable that overrides this setting ("" if none)
	Class       ConfigClass
	// Explicit reports whether an operator actually supplied this value —
	// the env var is set, or the key was present in the YAML file — as
	// opposed to it being the struct default that every key carries.
	//
	// The settings service needs this to decide precedence. Source alone
	// cannot: it reports "config" both for "written in the file" and for
	// "nobody ever set this", and treating the latter as configured is what
	// made every operational database value unreachable (#415).
	Explicit bool
}

// IsSecret reports whether this setting's value must never be shown in an API
// response or written to a log.
//
// Consult this rather than either flag directly. The two are NOT equivalent and
// the difference is a live trap: Class.Secret is false for the whole
// `auth.oauth.providers.`, `auth.saml.providers.` and `content_oauth.providers.`
// subtrees (see prefixClassifications — a blanket true there would mis-mask
// non-secret sub-keys like .client_id), so for those subtrees secrecy is
// carried ONLY by the per-setting Secret flag that the provider helpers set on
// .client_secret, .sp_private_key and .idp_metadata_b64xml. The
// Class.Secret->Secret sync in GetMigratableSettings runs in one direction
// only, so checking Class.Secret alone silently treats a real OAuth client
// secret as public.
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: report whether a setting's value must be masked in responses and logs (pure)
func (m MigratableSetting) IsSecret() bool {
	return m.Secret || m.Class.Secret
}

// settingSource returns "environment" if the given env var is set, otherwise "config".
//
// The env var named here must be one this package's struct tags actually bind.
// Reporting "environment" for a name the struct does not read would mark the
// struct DEFAULT as Explicit, and an Explicit default outranks the database —
// turning a fallback into an override.
// SEM@ef979cc7527137e0448782341a4ffe8e944571f5: return 'environment' when the given env var is set, otherwise 'config' (pure)
func settingSource(envVar string) string {
	if os.Getenv(envVar) != "" {
		return "environment"
	}
	return "config"
}

// isOmittableEmptyValue reports whether value is the "unset" representation
// for a setting of the given canonical Type, for the purposes of
// OmitWhenEmpty (see its doc comment on SettingDef). The zero-value string
// differs by type: "" for string/bool/float, "0" for int (every current
// OmitWhenEmpty int-typed key used ">0" as its pre-registry emission guard,
// so "0" is the correct empty sentinel for them — a future int OmitWhenEmpty
// setting where 0 is a meaningful non-empty value would need its own
// handling here), and "[]" or "null" for json (json.Marshal renders a nil
// slice as "null" and an explicitly empty one as "[]").
func isOmittableEmptyValue(settingType, value string) bool {
	switch settingType {
	case "int":
		return value == "0"
	case "json":
		return value == "[]" || value == "null"
	default:
		return value == ""
	}
}

// classifyGenerated applies prefix classification to per-provider generated
// settings (auth.oauth.providers.*, auth.saml.providers.*,
// content_oauth.providers.*): these have dynamic cardinality and no
// SettingDef by design, so they carry no Class of their own until this runs.
// Mirrors GetMigratableSettings' historic one-direction Class.Secret->Secret
// sync — see IsSecret's doc comment for why the per-setting Secret flag set
// by the provider helpers below must not be cleared here.
func classifyGenerated(settings []MigratableSetting) []MigratableSetting {
	for i := range settings {
		settings[i].Class = classificationFor(settings[i].Key)
		if settings[i].Class.Secret {
			settings[i].Secret = true
		}
	}
	return settings
}

// GetMigratableSettings returns every setting that has a config-file or
// environment delivery path, with its current value read from this Config.
//
// This projects the registry (setting_defs_*.go) rather than maintaining a
// parallel list. A def with OmitWhenEmpty is left out entirely when its
// value is empty, reproducing the pre-registry builders' conditional
// emission (see OmitWhenEmpty's doc comment on SettingDef for why this
// matters beyond cosmetics). server.tls_cert_file and server.tls_key_file
// are special-cased instead of using OmitWhenEmpty: their old emission guard
// was server.tls_enabled, a different field than the one being emitted, and
// OmitWhenEmpty can only test a def's own Get() output.
//
// auth.oauth.providers.*, auth.saml.providers.* and content_oauth.providers.*
// are generated per configured provider rather than declared statically —
// dynamic cardinality means no fixed dotted key — and are appended after the
// registry projection.
// SEM@10b74985ed52c143cb0fb6e853b2d5f106de198f: list all config settings eligible for database migration with classification applied (pure)
func (c *Config) GetMigratableSettings() []MigratableSetting {
	defs := AllSettingDefs()
	settings := make([]MigratableSetting, 0, len(defs))

	for _, d := range defs {
		if d.Get == nil {
			continue // database-only: no config path to read from
		}
		if (d.Key == "server.tls_cert_file" || d.Key == "server.tls_key_file") && !c.Server.TLSEnabled {
			continue
		}
		value := d.Get(c)
		if d.OmitWhenEmpty && isOmittableEmptyValue(d.Type, value) {
			continue
		}
		settings = append(settings, MigratableSetting{
			Key:         d.Key,
			Value:       value,
			Type:        d.Type,
			Description: d.Description,
			Secret:      d.Class.Secret,
			Source:      settingSource(d.EnvVar),
			EnvVar:      d.EnvVar,
			Class:       d.Class,
		})
	}

	settings = append(settings, classifyGenerated(c.getMigratableOAuthSettings())...)
	settings = append(settings, classifyGenerated(c.getMigratableSAMLSettings())...)
	settings = append(settings, classifyGenerated(c.getMigratableContentOAuthSettings())...)

	for i := range settings {
		// Explicit = an operator actually supplied this value, rather than it
		// being the struct default every key carries. Source already reports
		// "environment" when the env var is set; explicitFileKeys records the
		// keys the YAML file named. See the Explicit field's doc comment for
		// why Source alone is not enough.
		settings[i].Explicit = settings[i].Source == "environment" ||
			c.explicitFileKeys[settings[i].Key]
	}

	return settings
}

// getMigratableOAuthSettings returns settings generated per configured OAuth
// provider (auth.oauth.providers.*). auth.oauth_callback_url and
// auth.oauth.client_callback_allowlist are now projected from the registry
// in GetMigratableSettings; this covers only the provider subtree, which has
// dynamic cardinality and no SettingDef by design.
func (c *Config) getMigratableOAuthSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	for providerKey, p := range c.Auth.OAuth.Providers {
		if !p.Enabled {
			continue
		}
		settings = append(settings, c.getMigratableOAuthProviderSettings(providerKey, p)...)
	}

	return settings
}

// getMigratableOAuthProviderSettings returns settings for a single OAuth provider
// SEM@ef979cc7527137e0448782341a4ffe8e944571f5: build migratable settings list for a single OAuth provider's credentials and metadata (pure)
func (c *Config) getMigratableOAuthProviderSettings(providerKey string, p OAuthProviderConfig) []MigratableSetting {
	prefix := "auth.oauth.providers." + providerKey
	settings := []MigratableSetting{
		{Key: prefix + ".enabled", Value: "true", Type: "bool", Description: "OAuth provider enabled", Source: "config"},
	}

	// Add non-empty string fields
	stringFields := []struct {
		suffix, value, desc string
	}{
		{".id", p.ID, "OAuth provider ID"},
		{".name", p.Name, "OAuth provider display name"},
		{".icon", p.Icon, "OAuth provider icon"},
		{".authorization_url", p.AuthorizationURL, "OAuth authorization URL"},
		{".token_url", p.TokenURL, "OAuth token URL"},
		{".issuer", p.Issuer, "OAuth issuer"},
		{".jwks_url", p.JWKSURL, "OAuth JWKS URL"},
		{".client_id", p.ClientID, "OAuth client ID"}, // semi-public, visible in browser
	}

	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{
				Key: prefix + f.suffix, Value: f.value, Type: "string", Description: f.desc, Source: "config",
			})
		}
	}

	// Client secret — masked in API responses
	settings = append(settings, MigratableSetting{
		Key: prefix + ".client_secret", Value: p.ClientSecret, Type: "string",
		Description: "OAuth client secret", Source: "config", Secret: true,
	})

	// Scopes as JSON array
	if len(p.Scopes) > 0 {
		scopesJSON, _ := json.Marshal(p.Scopes)
		settings = append(settings, MigratableSetting{
			Key: prefix + ".scopes", Value: string(scopesJSON), Type: "json",
			Description: "OAuth scopes", Source: "config",
		})
	}

	// UserInfo endpoints as JSON
	if len(p.UserInfo) > 0 {
		userInfoJSON, _ := json.Marshal(p.UserInfo)
		settings = append(settings, MigratableSetting{
			Key: prefix + ".userinfo", Value: string(userInfoJSON), Type: "json",
			Description: "OAuth userinfo endpoints", Source: "config",
		})
	}

	if p.AuthHeaderFormat != "" {
		settings = append(settings, MigratableSetting{
			Key: prefix + ".auth_header_format", Value: p.AuthHeaderFormat, Type: "string",
			Description: "OAuth auth header format", Source: "config",
		})
	}
	if p.AcceptHeader != "" {
		settings = append(settings, MigratableSetting{
			Key: prefix + ".accept_header", Value: p.AcceptHeader, Type: "string",
			Description: "OAuth accept header", Source: "config",
		})
	}

	return settings
}

// getMigratableSAMLSettings returns SAML provider settings
// SEM@f25790d896e8e128807a3c9a0a517fcbe6f710fe: build migratable settings list for all enabled SAML providers (pure)
func (c *Config) getMigratableSAMLSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	if !c.Auth.SAML.Enabled {
		return settings
	}

	for providerKey, p := range c.Auth.SAML.Providers {
		if !p.Enabled {
			continue
		}
		settings = append(settings, c.getMigratableSAMLProviderSettings(providerKey, p)...)
	}

	return settings
}

// getMigratableSAMLProviderSettings returns settings for a single SAML provider
// SEM@78155d54490599e00095eb72b817575bb1e8da5b: build migratable settings list for a single SAML provider's endpoints, flags, and secrets (pure)
func (c *Config) getMigratableSAMLProviderSettings(providerKey string, p SAMLProviderConfig) []MigratableSetting {
	prefix := "auth.saml.providers." + providerKey
	settings := []MigratableSetting{
		{Key: prefix + ".enabled", Value: "true", Type: "bool", Description: "SAML provider enabled", Source: "config"},
	}

	// Add non-empty string fields
	stringFields := []struct {
		suffix, value, desc string
	}{
		{".id", p.ID, "SAML provider ID"},
		{".name", p.Name, "SAML provider display name"},
		{".icon", p.Icon, "SAML provider icon"},
		{".entity_id", p.EntityID, "SAML SP entity ID"},
		{".metadata_url", p.MetadataURL, "SAML metadata URL"},
		{".acs_url", p.ACSURL, "SAML ACS URL"},
		{".slo_url", p.SLOURL, "SAML SLO URL"},
		{".idp_metadata_url", p.IDPMetadataURL, "SAML IdP metadata URL"},
		{".name_id_attribute", p.NameIDAttribute, "SAML NameID attribute"},
		{".email_attribute", p.EmailAttribute, "SAML email attribute"},
		{".name_attribute", p.NameAttribute, "SAML name attribute"},
		{".given_name_attribute", p.GivenNameAttribute, "SAML given name attribute"},
		{".family_name_attribute", p.FamilyNameAttribute, "SAML family name attribute"},
		{".groups_attribute", p.GroupsAttribute, "SAML groups attribute"},
	}

	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{
				Key: prefix + f.suffix, Value: f.value, Type: "string", Description: f.desc, Source: "config",
			})
		}
	}

	// SAML behavior flags (always include these)
	boolFields := []struct {
		suffix string
		value  bool
		desc   string
	}{
		{".allow_idp_initiated", p.AllowIDPInitiated, "Allow IdP-initiated SAML login"},
		{".force_authn", p.ForceAuthn, "Force re-authentication"},
		{".sign_requests", p.SignRequests, "Sign SAML requests"},
	}

	for _, f := range boolFields {
		settings = append(settings, MigratableSetting{
			Key: prefix + f.suffix, Value: strconv.FormatBool(f.value), Type: "bool", Description: f.desc, Source: "config",
		})
	}

	// Secret fields
	settings = append(settings,
		MigratableSetting{Key: prefix + ".sp_private_key", Value: p.SPPrivateKey, Type: "string", Description: "SAML SP private key", Source: "config", Secret: true},
		MigratableSetting{Key: prefix + ".sp_certificate", Value: p.SPCertificate, Type: "string", Description: "SAML SP certificate", Source: "config", Secret: true},
		MigratableSetting{Key: prefix + ".idp_metadata_b64xml", Value: p.IDPMetadataB64XML, Type: "string", Description: "IdP metadata (base64 XML)", Source: "config", Secret: true},
	)

	return settings
}

// DefaultOperationalSettings returns the operational-category settings from a
// default Config. It is the seed source for the DB-backed settings service.
// Bootstrap-category settings are intentionally excluded — they are file/env
// only and must never be written to the database.
// SEM@ab3b70da0bebdbb7db5fefc97e15036dc3f5c4c5: return operational-category migratable settings from a default config, excluding bootstrap-only settings (pure)
func DefaultOperationalSettings() []MigratableSetting {
	all := getDefaultConfig().GetMigratableSettings()
	out := make([]MigratableSetting, 0, len(all))
	for _, s := range all {
		if s.Class.Category == CategoryOperational {
			out = append(out, s)
		}
	}
	return out
}

// getMigratableContentOAuthSettings returns settings generated per configured
// content OAuth provider (content_oauth.providers.*). content_oauth.callback_url
// is now projected from the registry in GetMigratableSettings; this covers
// only the provider subtree, which has dynamic cardinality and no SettingDef
// by design.
func (c *Config) getMigratableContentOAuthSettings() []MigratableSetting {
	settings := []MigratableSetting{}
	for providerKey, p := range c.ContentOAuth.Providers {
		if !p.Enabled {
			continue
		}
		settings = append(settings, getMigratableContentOAuthProviderSettings(providerKey, p)...)
	}
	return settings
}

// getMigratableContentOAuthProviderSettings returns settings for a single content OAuth provider.
// SEM@926d7b92ff82030ff3a1c03b2307247302613d38: build migratable settings list for a single content OAuth provider's URLs, client credentials, and scopes (pure)
func getMigratableContentOAuthProviderSettings(providerKey string, p ContentOAuthProviderConfig) []MigratableSetting {
	prefix := "content_oauth.providers." + providerKey
	settings := []MigratableSetting{
		{Key: prefix + ".enabled", Value: "true", Type: "bool", Description: "Content OAuth provider enabled", Source: "config"},
	}
	stringFields := []struct{ suffix, value, desc string }{
		{".name", p.Name, "Content OAuth provider display name"},
		{".icon", p.Icon, "Content OAuth provider icon"},
		{".client_id", p.ClientID, "Content OAuth provider client ID"},
		{".auth_url", p.AuthURL, "Content OAuth authorization URL"},
		{".token_url", p.TokenURL, "Content OAuth token URL"},
		{".userinfo_url", p.UserinfoURL, "Content OAuth userinfo URL"},
		{".revocation_url", p.RevocationURL, "Content OAuth token revocation URL"},
	}
	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{
				Key: prefix + f.suffix, Value: f.value, Type: "string", Description: f.desc, Source: "config",
			})
		}
	}
	// Client secret — masked in API responses. Guarded with a non-empty check
	// like its sibling string fields so an enabled provider with an empty
	// ClientSecret never emits an empty-valued setting (Oracle empty-CLOB safe
	// at the source, rather than relying solely on downstream skip guards).
	if p.ClientSecret != "" {
		settings = append(settings, MigratableSetting{
			Key: prefix + ".client_secret", Value: p.ClientSecret, Type: "string",
			Description: "Content OAuth client secret", Source: "config", Secret: true,
		})
	}
	if len(p.RequiredScopes) > 0 {
		scopesJSON, _ := json.Marshal(p.RequiredScopes)
		settings = append(settings, MigratableSetting{
			Key: prefix + ".required_scopes", Value: string(scopesJSON), Type: "json",
			Description: "Content OAuth required scopes", Source: "config",
		})
	}
	return settings
}
