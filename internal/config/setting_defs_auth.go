package config

import (
	"encoding/json"
	"strconv"
)

// authSettingDefs declares the authentication and authorization settings.
var authSettingDefs = []SettingDef{
	{
		Key:         "auth.build_mode",
		Class:       bootstrapClass(true, VisibilityInternal, false),
		Type:        "string",
		Description: "Build mode (dev, test, production)",
		YAMLPath:    "auth.build_mode",
		EnvVar:      "TMI_BUILD_MODE",
		Get:         func(c *Config) string { return c.Auth.BuildMode },
	},
	{
		Key:         "auth.jwt.secret",
		Class:       bootstrapClass(true, VisibilityInternal, true),
		Type:        "string",
		Description: "JWT signing secret",
		YAMLPath:    "auth.jwt.secret",
		EnvVar:      "TMI_JWT_SECRET",
		Get:         func(c *Config) string { return c.Auth.JWT.Secret },
	},
	{
		Key:         "auth.jwt.signing_method",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "JWT signing method",
		YAMLPath:    "auth.jwt.signing_method",
		EnvVar:      "TMI_JWT_SIGNING_METHOD",
		Get:         func(c *Config) string { return c.Auth.JWT.SigningMethod },
	},
	// The eight settings below (auto_promote_first_user through
	// cookie.secure) are Static: each is read directly off the boot-time
	// *Config struct by the code that consumes it — cmd/server/jwt_auth.go's
	// AutoPromoteFirstUser check, auth/service.go's JWT issuance
	// (ExpirationSeconds, RefreshTokenDays, SessionLifetimeDays),
	// cmd/server/main.go's step-up-window and cookie-manager construction
	// (StepUpWindowSeconds, Cookie.Enabled/Domain/Secure) — with no
	// RuntimeConfigReader (or equivalent settings-service lookup) wired for
	// any of them. Contrast with auth.everyone_is_a_reviewer directly below,
	// which stays Hot: it has a DB-backed reader
	// (JWTAuthenticator.everyoneIsAReviewer, memoized 60s).
	{
		Key:          "auth.auto_promote_first_user",
		Class:        withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:         "bool",
		Description:  "Auto-promote first user to admin",
		Default:      "false",
		YAMLPath:     "auth.auto_promote_first_user",
		EnvVar:       "TMI_AUTH_AUTO_PROMOTE_FIRST_USER",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Auth.AutoPromoteFirstUser) },
		Transitional: true,
	},
	{
		Key:          "auth.everyone_is_a_reviewer",
		Class:        operationalClass(VisibilityAdminOnly, false),
		Type:         "bool",
		Description:  "Auto-add all users to Security Reviewers group",
		Default:      "false",
		YAMLPath:     "auth.everyone_is_a_reviewer",
		EnvVar:       "TMI_AUTH_EVERYONE_IS_A_REVIEWER",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Auth.EveryoneIsAReviewer) },
		Transitional: true,
	},
	{
		Key:          "auth.jwt.expiration_seconds",
		Class:        withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:         "int",
		Description:  "JWT token expiration in seconds",
		Default:      "3600",
		YAMLPath:     "auth.jwt.expiration_seconds",
		EnvVar:       "TMI_JWT_EXPIRATION_SECONDS",
		Get:          func(c *Config) string { return strconv.Itoa(c.Auth.JWT.ExpirationSeconds) },
		Transitional: true,
	},
	{
		Key:          "auth.jwt.refresh_token_days",
		Class:        withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:         "int",
		Description:  "Refresh token TTL in days",
		Default:      "7",
		YAMLPath:     "auth.jwt.refresh_token_days",
		EnvVar:       "TMI_REFRESH_TOKEN_DAYS",
		Get:          func(c *Config) string { return strconv.Itoa(c.Auth.JWT.RefreshTokenDays) },
		Transitional: true,
	},
	{
		Key:          "auth.jwt.session_lifetime_days",
		Class:        withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:         "int",
		Description:  "Absolute session lifetime in days",
		Default:      "7",
		YAMLPath:     "auth.jwt.session_lifetime_days",
		EnvVar:       "TMI_SESSION_LIFETIME_DAYS",
		Get:          func(c *Config) string { return strconv.Itoa(c.Auth.JWT.SessionLifetimeDays) },
		Transitional: true,
	},
	{
		Key:          "auth.step_up_window_seconds",
		Class:        withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:         "int",
		Description:  "Step-up auth_time freshness window in seconds for /admin/* writes (#355); minimum 60",
		Default:      "300",
		YAMLPath:     "auth.step_up_window_seconds",
		EnvVar:       "TMI_AUTH_STEP_UP_WINDOW_SECONDS",
		Get:          func(c *Config) string { return strconv.Itoa(c.Auth.StepUpWindowSeconds) },
		Transitional: true,
	},
	{
		Key:          "auth.cookie.enabled",
		Class:        withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:         "bool",
		Description:  "HttpOnly cookie-based auth enabled",
		Default:      "true",
		YAMLPath:     "auth.cookie.enabled",
		EnvVar:       "TMI_COOKIE_ENABLED",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Auth.Cookie.Enabled) },
		Transitional: true,
	},
	{
		Key:          "auth.cookie.domain",
		Class:        withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:         "string",
		Description:  "Cookie domain",
		Default:      "",
		YAMLPath:     "auth.cookie.domain",
		EnvVar:       "TMI_COOKIE_DOMAIN",
		Get:          func(c *Config) string { return c.Auth.Cookie.Domain },
		Transitional: true,
	},
	{
		Key:          "auth.cookie.secure",
		Class:        withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:         "bool",
		Description:  "Require HTTPS for cookies",
		Default:      "false",
		YAMLPath:     "auth.cookie.secure",
		EnvVar:       "TMI_COOKIE_SECURE",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Auth.Cookie.Secure) },
		Transitional: true,
	},
	{
		// Key differs from YAMLPath: the setting key is flat
		// ("auth.oauth_callback_url"), matching the pre-existing migratable
		// setting key, while the struct path goes through the nested OAuth
		// config ("auth.oauth.callback_url").
		Key:           "auth.oauth_callback_url",
		Class:         operationalClass(VisibilityAdminOnly, false),
		Type:          "string",
		Description:   "OAuth callback URL",
		Default:       "http://localhost:8080/oauth2/callback",
		YAMLPath:      "auth.oauth.callback_url",
		EnvVar:        "TMI_OAUTH_CALLBACK_URL",
		Get:           func(c *Config) string { return c.Auth.OAuth.CallbackURL },
		Transitional:  true,
		OmitWhenEmpty: true,
	},
	{
		Key:         "auth.oauth.client_callback_allowlist",
		Class:       operationalClass(VisibilityAdminOnly, false),
		Type:        "json",
		Description: "Allowlist of client_callback URLs for /oauth2/authorize and /oauth2/step_up (exact URL or wildcard pattern ending in '*')",
		Default:     "[]",
		YAMLPath:    "auth.oauth.client_callback_allowlist",
		EnvVar:      "TMI_OAUTH_CLIENT_CALLBACK_ALLOWLIST",
		Get: func(c *Config) string {
			if len(c.Auth.OAuth.ClientCallbackAllowList) == 0 {
				return "[]"
			}
			b, err := json.Marshal(c.Auth.OAuth.ClientCallbackAllowList)
			if err != nil {
				return "[]"
			}
			return string(b)
		},
		Transitional:  true,
		OmitWhenEmpty: true,
	},
	{
		// Key differs from YAMLPath: the setting key is the legacy flat
		// feature-flag name ("features.saml_enabled"), while the struct path
		// is the nested SAML config's enabled flag ("auth.saml.enabled").
		//
		// Static: auth/service.go's NewService only builds service.samlManager
		// when config.SAML.Enabled is true at construction time (auth/service.go
		// ~line 148, "if config.SAML.Enabled { samlManager := NewSAMLManager(...) }").
		// A separate DB-backed reader (RuntimeConfigReader.IsSAMLEnabled) does
		// exist and gates the SAML HTTP handlers per request, but if the manager
		// was never constructed at boot, flipping the DB row does not make SAML
		// login work — samlManager stays nil until restart. The setting that
		// actually controls whether SAML functions is the boot-time one.
		Key:          "features.saml_enabled",
		Class:        withMutability(operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX), MutabilityStatic),
		Type:         "bool",
		Description:  "Enable SAML authentication",
		Default:      "false",
		YAMLPath:     "auth.saml.enabled",
		EnvVar:       "TMI_SAML_ENABLED",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Auth.SAML.Enabled) },
		Transitional: true,
		Seeded:       true,
	},
	{
		// Top-level Config field, not under Auth. No env tag exists on
		// Administrators — it is config-file only (config.go:49,
		// `yaml:"administrators"` with no `env:` tag), so EnvVar is
		// genuinely empty; YAMLPath is real and stays "administrators".
		//
		// Static: cmd/server/main.go's admin-init loop reads
		// cfg.Administrators exactly once at startup to seed/promote the
		// configured admins. There is no runtime re-read — a database edit
		// to this key does not add or remove an admin without a restart.
		Key:         "administrators",
		Class:       withMutability(operationalClass(VisibilityAdminOnly, false), MutabilityStatic),
		Type:        "json",
		Description: "Configured administrators",
		Default:     "[]",
		YAMLPath:    "administrators",
		EnvVar:      "",
		Get: func(c *Config) string {
			if len(c.Administrators) == 0 {
				return "[]"
			}
			b, err := json.Marshal(c.Administrators)
			if err != nil {
				return "[]"
			}
			return string(b)
		},
		Transitional:  true,
		OmitWhenEmpty: true,
	},
}
