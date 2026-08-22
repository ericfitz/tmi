package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validBootstrapDef() SettingDef {
	return SettingDef{
		Key:         "server.port",
		Type:        "string",
		Description: "HTTP server port",
		Class: ConfigClass{
			Category:   CategoryBootstrap,
			Visibility: VisibilityInternal,
			Mutability: MutabilityStatic,
			Consumers:  []Consumer{ConsumerMonolith},
		},
		YAMLPath: "server.port",
		EnvVar:   "TMI_SERVER_PORT",
		Get:      func(c *Config) string { return c.Server.Port },
	}
}

func validOperationalDef() SettingDef {
	return SettingDef{
		Key:         "ui.default_theme",
		Type:        "string",
		Description: "Default UI theme",
		Default:     "auto",
		Class: ConfigClass{
			Category:   CategoryOperational,
			Visibility: VisibilityPublic,
			Mutability: MutabilityHot,
			Delivery:   &Delivery{},
			Consumers:  []Consumer{ConsumerMonolith, ConsumerTMIUX},
		},
	}
}

func TestValidateSettingDefs_AcceptsValidSet(t *testing.T) {
	err := ValidateSettingDefs([]SettingDef{validBootstrapDef(), validOperationalDef()})
	assert.NoError(t, err)
}

func TestValidateSettingDefs_RejectsUnclassified(t *testing.T) {
	d := validBootstrapDef()
	d.Class.Category = CategoryUnclassified
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclassified")
}

func TestValidateSettingDefs_RejectsDuplicateKeys(t *testing.T) {
	err := ValidateSettingDefs([]SettingDef{validBootstrapDef(), validBootstrapDef()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestValidateSettingDefs_BootstrapRequiresEnvVarAndYAMLPath(t *testing.T) {
	d := validBootstrapDef()
	d.EnvVar = ""
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EnvVar")

	d = validBootstrapDef()
	d.YAMLPath = ""
	err = ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "YAMLPath")
}

func TestValidateSettingDefs_BootstrapRequiresGetter(t *testing.T) {
	d := validBootstrapDef()
	d.Get = nil
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Get")
}

func TestValidateSettingDefs_BootstrapMustNotCarryDelivery(t *testing.T) {
	d := validBootstrapDef()
	d.Class.Delivery = &Delivery{}
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Delivery")
}

func TestValidateSettingDefs_BootstrapMustNotBeTransitional(t *testing.T) {
	d := validBootstrapDef()
	d.Transitional = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Transitional")
}

func TestValidateSettingDefs_OperationalRequiresDefault(t *testing.T) {
	// Type "bool" here, not the "string" of validOperationalDef(): a
	// non-string zero value always serializes to a non-empty string
	// ("false"), so an empty Default on a bool setting is unambiguously a
	// missing declaration, not a legitimate zero value. See
	// TestValidateSettingDefs_StringOperationalDefaultMayBeEmpty for the
	// string-typed case, which this rule deliberately does not flag.
	d := validOperationalDef()
	d.Type = "bool"
	d.Default = ""
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Default")
}

func TestValidateSettingDefs_StringOperationalDefaultMayBeEmpty(t *testing.T) {
	// A string-typed setting's Default is exempt from the "must declare"
	// rule: Default is carried as a plain string, so there is no way to
	// distinguish a genuinely empty compiled-in default (e.g.
	// auth.cookie.domain) from one nobody filled in.
	d := validOperationalDef()
	d.Type = "string"
	d.Default = ""
	assert.NoError(t, ValidateSettingDefs([]SettingDef{d}))
}

func TestValidateSettingDefs_OperationalRequiresDelivery(t *testing.T) {
	d := validOperationalDef()
	d.Class.Delivery = nil
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Delivery")
}

func TestValidateSettingDefs_NonTransitionalOperationalMustHaveNoConfigPath(t *testing.T) {
	d := validOperationalDef()
	d.EnvVar = "TMI_UI_DEFAULT_THEME"
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-transitional")
}

func TestValidateSettingDefs_TransitionalOperationalRequiresConfigPath(t *testing.T) {
	d := validOperationalDef()
	d.Transitional = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transitional")
}

func TestValidateSettingDefs_TransitionalOperationalAcceptsConfigPath(t *testing.T) {
	d := validOperationalDef()
	d.Transitional = true
	d.EnvVar = "TMI_UI_DEFAULT_THEME"
	d.YAMLPath = "ui.default_theme"
	d.Get = func(c *Config) string { return "auto" }
	assert.NoError(t, ValidateSettingDefs([]SettingDef{d}))
}

func TestValidateSettingDefs_RejectsUnknownType(t *testing.T) {
	d := validBootstrapDef()
	d.Type = "widget"
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type")
}

func TestValidateSettingDefs_RequiresDescriptionAndConsumers(t *testing.T) {
	d := validBootstrapDef()
	d.Description = ""
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Description")

	d = validBootstrapDef()
	d.Class.Consumers = nil
	err = ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Consumers")
}

func TestValidateSettingDefs_PublicCannotBeSecret(t *testing.T) {
	d := validOperationalDef()
	d.Class.Secret = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "public")
}

func TestValidateSettingDefs_RequiredImpliesBootstrap(t *testing.T) {
	d := validOperationalDef()
	d.Class.Required = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Required")
}

func TestValidateSettingDefs_SharedInvariantImpliesStamped(t *testing.T) {
	d := validOperationalDef()
	d.Class.Delivery = &Delivery{SharedInvariant: true}
	d.Class.Consumers = []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed}
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StampedIntoEnvelope")
}

func TestValidateSettingDefs_SharedInvariantRequiresWorkerConsumer(t *testing.T) {
	d := validOperationalDef()
	d.Class.Delivery = &Delivery{StampedIntoEnvelope: true, SharedInvariant: true}
	d.Class.Consumers = []Consumer{ConsumerMonolith}
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker")
}

func TestValidateSettingDefs_SharedInvariantRequiresMonolithConsumer(t *testing.T) {
	d := validOperationalDef()
	d.Class.Delivery = &Delivery{StampedIntoEnvelope: true, SharedInvariant: true}
	d.Class.Consumers = []Consumer{ConsumerWorkerChunkEmbed}
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "monolith")
}

func TestValidateSettingDefs_ReferenceImpliesSecret(t *testing.T) {
	d := validBootstrapDef()
	d.Class.ValueKind = ValueKindReference
	d.Class.Secret = false
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference")
}

func TestValidateSettingDefs_SeededImpliesOperational(t *testing.T) {
	d := validBootstrapDef()
	d.Seeded = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Seeded")
}

func TestValidateSettingDefs_SeededRequiresDefault(t *testing.T) {
	// Type "string" is otherwise exempt from the general Default
	// requirement, but a Seeded key must still have a Default: it is
	// written into system_settings at database init regardless of type.
	d := validOperationalDef()
	d.Type = "string"
	d.Default = ""
	d.Seeded = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Seeded setting must declare a Default")
}

func TestValidateSettingDefs_SeededMustNotBeSecret(t *testing.T) {
	d := validOperationalDef()
	d.Class.Visibility = VisibilityAdminOnly // a public setting must not be Secret
	d.Class.Secret = true
	d.Seeded = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Seeded setting must not be Secret")
}

func TestValidateSettingDefs_SeededNonSecretWithDefaultIsValid(t *testing.T) {
	d := validOperationalDef()
	d.Seeded = true
	assert.NoError(t, ValidateSettingDefs([]SettingDef{d}))
}

func TestValidateSettingDefs_RegistryItselfIsValid(t *testing.T) {
	assert.NoError(t, ValidateSettingDefs(AllSettingDefs()))
}

func TestValidateSettingDefs_TransitionalOperationalYAMLPathOptional(t *testing.T) {
	d := validOperationalDef()
	d.Transitional = true
	d.EnvVar = "TMI_JWT_EXPIRATION_SECONDS"
	d.Get = func(c *Config) string { return "60" }
	// YAMLPath deliberately left empty, as for the derived
	// session.timeout_minutes key.
	assert.NoError(t, ValidateSettingDefs([]SettingDef{d}))
}

func TestValidateSettingDefs_TransitionalOperationalEnvVarOptional(t *testing.T) {
	d := validOperationalDef()
	d.Transitional = true
	d.YAMLPath = "administrators"
	d.Get = func(c *Config) string { return "[]" }
	// EnvVar deliberately left empty, as for administrators (config-file only,
	// no env: tag).
	assert.NoError(t, ValidateSettingDefs([]SettingDef{d}))
}

func TestValidateSettingDefs_TransitionalOperationalStillRequiresGet(t *testing.T) {
	d := validOperationalDef()
	d.Transitional = true
	d.YAMLPath = "ui.default_theme"
	d.EnvVar = "TMI_UI_DEFAULT_THEME"
	d.Get = nil
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Get")
}

func TestValidateSettingDefs_SecretOperationalDefaultOptional(t *testing.T) {
	d := validOperationalDef()
	d.Default = ""
	// A public setting must not be Secret, so switch visibility before
	// marking it secret.
	d.Class.Visibility = VisibilityAdminOnly
	d.Class.Secret = true
	assert.NoError(t, ValidateSettingDefs([]SettingDef{d}))
}

func TestValidateSettingDefs_NonSecretOperationalStillRequiresDefault(t *testing.T) {
	d := validOperationalDef()
	d.Type = "bool" // "string" is separately exempt; see the string-typed test above
	d.Default = ""
	d.Class.Secret = false
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Default")
}
