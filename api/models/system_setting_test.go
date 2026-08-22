package models

import (
	"encoding/json"
	"testing"
)

// TestSystemSetting_IsExplicit pins the #794 precedence rule and, critically,
// its polarity: "explicit" is the ONLY value that yields true. NULL, "seeded",
// and anything unexpected all read as not-explicit, so the database outranks a
// configured env/YAML value only when something deliberately said so.
//
// The polarity is the safety property. Every way an origin value can go
// missing — an empty string bound on Oracle, a writer that forgets the stamp,
// a stale pre-upgrade Redis entry with no origin key — lands on "not
// explicit", which makes the config layer win. Config is what an operator can
// see and control; a row nobody set winning is the 2026-08-20 outage.
func TestSystemSetting_IsExplicit(t *testing.T) {
	tests := []struct {
		name   string
		origin NullableDBVarchar
		want   bool
	}{
		{
			name:   "NULL origin is not explicit (fail-safe)",
			origin: NullableDBVarchar{Valid: false},
			want:   false,
		},
		{
			name:   "seeded origin is not explicit",
			origin: NullableDBVarchar{String: SystemSettingOriginSeeded, Valid: true},
			want:   false,
		},
		{
			name:   "explicit origin is explicit",
			origin: NullableDBVarchar{String: SystemSettingOriginExplicit, Valid: true},
			want:   true,
		},
		{
			name:   "unexpected origin value is not explicit (fail-safe)",
			origin: NullableDBVarchar{String: "some-future-value", Valid: true},
			want:   false,
		},
		{
			name:   "empty-string origin is not explicit (Oracle binds '' as NULL)",
			origin: NullableDBVarchar{String: "", Valid: true},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SystemSetting{Origin: tt.origin}
			if got := s.IsExplicit(); got != tt.want {
				t.Errorf("IsExplicit() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSystemSetting_OriginJSONRoundTrip guards against Origin regressing to
// json:"-". The Redis cache tier (SettingsService.setInRedisCache /
// getFromRedisCache) round-trips a SystemSetting through json.Marshal /
// json.Unmarshal; "-" would silently drop Origin on every Redis cache hit,
// making every seeded row look explicit after a warm read (oracle-db-admin
// review, #794). This is orthogonal to the wire API, which never marshals
// models.SystemSetting directly — modelToAPISystemSetting builds the API
// type field by field and never copies Origin across.
func TestSystemSetting_OriginJSONRoundTrip(t *testing.T) {
	original := SystemSetting{
		SettingKey:  "session.timeout_minutes",
		Value:       "60",
		SettingType: SystemSettingTypeInt,
		Origin:      NullableDBVarchar{String: SystemSettingOriginSeeded, Valid: true},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundTripped SystemSetting
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !roundTripped.Origin.Valid || roundTripped.Origin.String != SystemSettingOriginSeeded {
		t.Errorf("Origin after round-trip = (valid=%v, %q), want (valid=true, %q)",
			roundTripped.Origin.Valid, roundTripped.Origin.String, SystemSettingOriginSeeded)
	}
	if roundTripped.IsExplicit() {
		t.Error("round-tripped seeded row reports IsExplicit() == true; Origin was lost in JSON marshaling")
	}
}
