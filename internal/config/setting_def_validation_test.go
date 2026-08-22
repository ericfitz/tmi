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
	d := validOperationalDef()
	d.Default = ""
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Default")
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

func TestValidateSettingDefs_RegistryItselfIsValid(t *testing.T) {
	assert.NoError(t, ValidateSettingDefs(AllSettingDefs()))
}
