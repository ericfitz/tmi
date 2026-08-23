package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefFor_ReturnsRegisteredDef(t *testing.T) {
	defs := []SettingDef{
		{
			Key:         "test.example",
			Type:        "string",
			Description: "an example",
			Class: ConfigClass{
				Category:   CategoryBootstrap,
				Visibility: VisibilityInternal,
				Consumers:  []Consumer{ConsumerMonolith},
			},
			YAMLPath: "test.example",
			EnvVar:   "TMI_TEST_EXAMPLE",
			Get:      func(c *Config) string { return "v" },
		},
	}
	idx := indexDefs(defs)

	got, ok := idx["test.example"]
	require.True(t, ok)
	assert.Equal(t, "string", got.Type)
	assert.Equal(t, CategoryBootstrap, got.Class.Category)
}

func TestDefFor_UnknownKeyReturnsFalse(t *testing.T) {
	_, ok := DefFor("no.such.key.anywhere")
	assert.False(t, ok)
}

func TestAllSettingDefs_ReturnsACopy(t *testing.T) {
	a := AllSettingDefs()
	require.NotNil(t, a)
	if len(a) == 0 {
		t.Skip("registry not yet populated; covered from Task 3 onward")
	}
	a[0].Key = "mutated"
	b := AllSettingDefs()
	assert.NotEqual(t, "mutated", b[0].Key, "AllSettingDefs must not expose the backing array")
}
