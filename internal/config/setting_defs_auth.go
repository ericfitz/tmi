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
	{
		Key:          "auth.auto_promote_first_user",
		Class:        operationalClass(VisibilityAdminOnly, false),
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
		Class:        operationalClass(VisibilityAdminOnly, false),
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
		Class:        operationalClass(VisibilityAdminOnly, false),
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
		Class:        operationalClass(VisibilityAdminOnly, false),
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
		Class:        operationalClass(VisibilityAdminOnly, false),
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
		Class:        operationalClass(VisibilityAdminOnly, false),
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
		Class:        operationalClass(VisibilityAdminOnly, false),
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
		Class:        operationalClass(VisibilityAdminOnly, false),
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
		Key:          "auth.oauth_callback_url",
		Class:        operationalClass(VisibilityAdminOnly, false),
		Type:         "string",
		Description:  "OAuth callback URL",
		Default:      "http://localhost:8080/oauth2/callback",
		YAMLPath:     "auth.oauth.callback_url",
		EnvVar:       "TMI_OAUTH_CALLBACK_URL",
		Get:          func(c *Config) string { return c.Auth.OAuth.CallbackURL },
		Transitional: true,
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
		Transitional: true,
	},
	{
		// Key differs from YAMLPath: the setting key is the legacy flat
		// feature-flag name ("features.saml_enabled"), while the struct path
		// is the nested SAML config's enabled flag ("auth.saml.enabled").
		Key:          "features.saml_enabled",
		Class:        operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX),
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
		// Administrators — it is config-file only. YAMLPath is deliberately
		// left empty: the bijection test (setting_defs_bijection_test.go)
		// walks only env-tagged Config struct fields, so a non-empty
		// YAMLPath here — even though the "administrators" yaml key is real
		// — would register as a config path the struct walk never produces,
		// failing the test's "extra" check.
		Key:         "administrators",
		Class:       operationalClass(VisibilityAdminOnly, false),
		Type:        "json",
		Description: "Configured administrators",
		Default:     "[]",
		YAMLPath:    "",
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
		Transitional: true,
	},
}
