package config

import (
	"fmt"
	"strings"
)

// validSettingTypes is the closed set of canonical value types.
var validSettingTypes = map[string]bool{
	"string": true,
	"bool":   true,
	"int":    true,
	"float":  true,
	"json":   true,
}

// ValidateSettingDefs checks the registry's internal consistency and returns
// an error naming every problem found, one per line, or nil. It is the
// enforcement point for the rule that every setting must be classified: a
// definition whose Category is the zero value is rejected.
//
// This supersedes ValidateClassifications for the SettingDef registry: every
// rule that function enforces on a MigratableSetting is carried across here,
// plus the additional legality rules that apply to SettingDef's config-path
// (YAMLPath/EnvVar/Get) and Seeded fields.
func ValidateSettingDefs(defs []SettingDef) error {
	var problems []string
	add := func(key, msg string) {
		problems = append(problems, fmt.Sprintf("%s: %s", key, msg))
	}

	seen := make(map[string]bool, len(defs))
	for _, d := range defs {
		if d.Key == "" {
			problems = append(problems, "(empty key): a definition has no Key")
			continue
		}
		if seen[d.Key] {
			add(d.Key, "duplicate key in registry")
			continue
		}
		seen[d.Key] = true

		if d.Class.Category == CategoryUnclassified {
			add(d.Key, "unclassified — Category is the zero value")
			continue
		}

		validateGeneralRules(d, add)

		switch d.Class.Category {
		case CategoryBootstrap:
			validateBootstrapRules(d, add)
		case CategoryOperational:
			validateOperationalRules(d, add)
		default:
			add(d.Key, fmt.Sprintf("unknown Category value %d — update ValidateSettingDefs", d.Class.Category))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid setting definitions:\n  %s", strings.Join(problems, "\n  "))
}

// validateGeneralRules checks the rules that apply regardless of category.
func validateGeneralRules(d SettingDef, add func(key, msg string)) {
	c := d.Class

	if d.Description == "" {
		add(d.Key, "empty Description")
	}
	if len(c.Consumers) == 0 {
		add(d.Key, "no Consumers declared")
	}
	if !validSettingTypes[d.Type] {
		add(d.Key, fmt.Sprintf("unknown type %q", d.Type))
	}
	if c.ValueKind == ValueKindReference && !c.Secret {
		add(d.Key, "a reference value kind is only valid on a Secret setting")
	}
	if c.Visibility == VisibilityPublic && c.Secret {
		add(d.Key, "a public setting must not be Secret")
	}
	if c.Required && c.Category != CategoryBootstrap {
		add(d.Key, "Required implies bootstrap")
	}
	if d.Seeded && c.Category != CategoryOperational {
		add(d.Key, "Seeded implies CategoryOperational — a bootstrap setting must never be Seeded")
	}
	// Only Seeded keys are ever written into system_settings at database
	// init (models.DefaultSystemSettings), so Default is load-bearing
	// specifically for them — this is narrower than, and independent of, the
	// general "operational setting must declare a Default" rule below, which
	// exempts Type "string" and Class.Secret. A Seeded setting must still
	// have something to seed regardless of Type or Secret.
	if d.Seeded && d.Default == "" {
		add(d.Key, "Seeded setting must declare a Default — it is written into system_settings at database init")
	}
	// A secret must never be seeded into system_settings with a compiled-in
	// value: that would either be an empty placeholder (useless) or a real
	// hardcoded credential (never acceptable) landing in the database at
	// every install.
	if d.Seeded && c.Secret {
		add(d.Key, "Seeded setting must not be Secret — a secret must never be seeded into system_settings with a compiled-in value")
	}
}

// validateBootstrapRules checks the rules specific to CategoryBootstrap.
func validateBootstrapRules(d SettingDef, add func(key, msg string)) {
	if d.Class.Delivery != nil {
		add(d.Key, "bootstrap setting must not carry a Delivery")
	}
	if d.Transitional {
		add(d.Key, "bootstrap setting must not be Transitional")
	}
	if d.YAMLPath == "" {
		add(d.Key, "bootstrap setting must declare a YAMLPath")
	}
	if d.EnvVar == "" {
		add(d.Key, "bootstrap setting must declare an EnvVar")
	}
	if d.Get == nil {
		add(d.Key, "bootstrap setting must declare a Get accessor")
	}
}

// validateOperationalRules checks the rules specific to CategoryOperational.
func validateOperationalRules(d SettingDef, add func(key, msg string)) {
	c := d.Class

	if c.Delivery == nil {
		add(d.Key, "operational setting must carry a Delivery")
	}
	// A secret with no compiled-in default is correct: the alternatives are an
	// empty string this rule would otherwise reject, or a hardcoded
	// credential, which is never acceptable.
	//
	// A string-typed setting is exempted the same way for a different reason:
	// Default is carried as a plain string, so for Type "string" there is no
	// way to distinguish "the compiled-in default genuinely is empty"
	// (auth.cookie.domain, every unconfigured Timmy/content-source provider
	// field, the fail-closed ssrf.*.allowlist/schemes keys) from "nobody
	// filled this in". Every other type's zero value serializes to a
	// non-empty string ("false", "0", "[]"), so the check stays meaningful
	// there.
	if d.Default == "" && !c.Secret && d.Type != "string" {
		add(d.Key, "operational setting must declare a Default")
	}
	if c.Delivery != nil && c.Delivery.SharedInvariant {
		if !c.Delivery.StampedIntoEnvelope {
			add(d.Key, "SharedInvariant requires StampedIntoEnvelope")
		}
		if !hasWorkerConsumer(c.Consumers) {
			add(d.Key, "SharedInvariant requires at least one worker Consumer")
		}
		if !hasConsumer(c.Consumers, ConsumerMonolith) {
			add(d.Key, "SharedInvariant requires the monolith as a Consumer")
		}
	}

	hasConfigPath := d.YAMLPath != "" || d.EnvVar != "" || d.Get != nil
	if d.Transitional {
		// YAMLPath and EnvVar are each independently optional: administrators
		// has no env: tag at all (config.go assembles it imperatively in
		// Load()), and the derived session.timeout_minutes key has no
		// YAMLPath of its own. Get is the one thing every transitional
		// setting must still retain until Phase E removes it.
		if d.Get == nil {
			add(d.Key, "transitional operational setting must declare a Get accessor")
		}
	} else if hasConfigPath {
		add(d.Key, "non-transitional operational setting must have no YAMLPath, EnvVar or Get")
	}
}
