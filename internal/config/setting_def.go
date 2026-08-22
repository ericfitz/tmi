package config

// SettingDef is the single authoritative declaration of one configuration
// setting. Every setting the server can read — bootstrap or operational,
// config-delivered or database-seeded — has exactly one SettingDef.
//
// Which fields are legal depends on Class.Category; ValidateSettingDefs
// enforces that (see setting_def_validation.go).
type SettingDef struct {
	// Key is the canonical dotted setting key, e.g. "server.port".
	Key string
	// Class is the full classification: category, visibility, secrecy,
	// mutability, consumers, delivery.
	Class ConfigClass
	// Type is the value's canonical type: "string", "bool", "int",
	// "float", or "json".
	Type string
	// Description is human-readable and required; it feeds generated docs
	// and the admin API.
	Description string
	// Default is the canonical string form of the compiled-in default.
	// Required for operational settings, which have no config file to
	// fall back to.
	Default string

	// YAMLPath, EnvVar and Get are populated only for settings currently
	// delivered by config file or environment: all bootstrap settings, plus
	// operational settings still in transition (see Transitional).
	YAMLPath string
	EnvVar   string
	// Get extracts this setting's current value from a loaded Config as a
	// canonical string. Nil for settings with no config-file path.
	Get func(*Config) string

	// Transitional marks an operational setting that still has a config/env
	// delivery path during the Phase A-E cutover described in the spec.
	// Every Transitional entry is scheduled for removal in Phase E; the
	// ratchet test in Task 9 prevents new ones from being added.
	Transitional bool

	// Seeded marks an operational setting that is written into the
	// system_settings table on database initialisation. It is deliberately
	// explicit rather than inferred: the set of seeded keys does not
	// coincide with any other property of a setting, and inferring it wrong
	// would either seed rows nobody set or stop seeding rows the server
	// expects. Only operational settings may be Seeded.
	Seeded bool
}

// IsSecret reports whether this setting's value must never appear in an API
// response or a log line.
//
// This answers the question only for STATICALLY DECLARED settings. The
// per-provider keys under auth.oauth.providers., auth.saml.providers. and
// content_oauth.providers. are generated per configured provider and have no
// SettingDef; for those, Class.Secret is deliberately false (a blanket true
// would mis-mask non-secret sub-keys like .client_id) and secrecy is carried
// only by the per-setting Secret flag that the provider helpers set on
// .client_secret, .sp_private_key and .idp_metadata_b64xml. Never reach for
// this method to decide secrecy for a provider key — use
// MigratableSetting.IsSecret(), which ORs both flags.
func (d SettingDef) IsSecret() bool {
	return d.Class.Secret
}

// settingDefs is the authoritative registry. Populated in Tasks 3-5.
var settingDefs []SettingDef

// settingDefIndex is the by-key lookup, built once from settingDefs.
var settingDefIndex = indexDefs(settingDefs)

// indexDefs builds a by-key lookup map from a slice of definitions.
func indexDefs(defs []SettingDef) map[string]SettingDef {
	idx := make(map[string]SettingDef, len(defs))
	for _, d := range defs {
		idx[d.Key] = d
	}
	return idx
}

// DefFor returns the declaration for a setting key, and whether one exists.
func DefFor(key string) (SettingDef, bool) {
	d, ok := settingDefIndex[key]
	return d, ok
}

// AllSettingDefs returns a copy of the registry, so callers cannot mutate it.
func AllSettingDefs() []SettingDef {
	out := make([]SettingDef, len(settingDefs))
	copy(out, settingDefs)
	return out
}
