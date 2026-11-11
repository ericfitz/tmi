package config

import "sort"

// SeedableOperationalDefs returns the operational settings that should be
// seeded into system_settings on database initialisation, sorted by key.
//
// This is the single source for the seed list. DefaultSystemSettings() in
// api/models projects it, rather than maintaining a parallel list — the
// parallel list is what left rate_limit.requests_per_minute and
// rate_limit.requests_per_hour seeded but unclassified (#809).
//
// Filtering on Seeded, not just CategoryOperational, is deliberate: the
// registry holds far more operational defs (config/env-delivered knobs like
// websocket.inactivity_timeout_seconds) than are ever written into
// system_settings. Seeded marks exactly the subset that is.
func SeedableOperationalDefs() []SettingDef {
	var out []SettingDef
	for _, d := range settingDefs {
		if d.Class.Category == CategoryOperational && d.Seeded {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
