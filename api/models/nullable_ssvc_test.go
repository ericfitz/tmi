package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNullableSSVC_Value(t *testing.T) {
	t.Run("valid SSVC returns JSON string", func(t *testing.T) {
		s := NullableSSVC{
			SSVCScore: SSVCScore{
				Vector:      "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/",
				Decision:    "Immediate",
				Methodology: "Supplier",
			},
			Valid: true,
		}
		val, err := s.Value()
		require.NoError(t, err)
		require.NotNil(t, val)
		str, ok := val.(string)
		require.True(t, ok)
		assert.Contains(t, str, `"vector"`)
		assert.Contains(t, str, `"decision"`)
		assert.Contains(t, str, `"methodology"`)
		assert.Contains(t, str, "Immediate")
		assert.Contains(t, str, "Supplier")
	})

	t.Run("invalid SSVC returns nil", func(t *testing.T) {
		s := NullableSSVC{Valid: false}
		val, err := s.Value()
		require.NoError(t, err)
		assert.Nil(t, val)
	})
}

func TestNullableSSVC_Scan(t *testing.T) {
	t.Run("scan valid JSON string", func(t *testing.T) {
		var s NullableSSVC
		err := s.Scan(`{"vector":"SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/","decision":"Immediate","methodology":"Supplier"}`)
		require.NoError(t, err)
		assert.True(t, s.Valid)
		assert.Equal(t, "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/", s.Vector)
		assert.Equal(t, "Immediate", s.Decision)
		assert.Equal(t, "Supplier", s.Methodology)
	})

	t.Run("scan valid JSON bytes", func(t *testing.T) {
		var s NullableSSVC
		err := s.Scan([]byte(`{"vector":"SSVCv2/E:A","decision":"Defer","methodology":"Supplier"}`))
		require.NoError(t, err)
		assert.True(t, s.Valid)
		assert.Equal(t, "Defer", s.Decision)
	})

	t.Run("scan nil sets invalid", func(t *testing.T) {
		s := NullableSSVC{Valid: true}
		err := s.Scan(nil)
		require.NoError(t, err)
		assert.False(t, s.Valid)
		assert.Equal(t, SSVCScore{}, s.SSVCScore)
	})

	t.Run("scan empty string sets invalid", func(t *testing.T) {
		var s NullableSSVC
		err := s.Scan("")
		require.NoError(t, err)
		assert.False(t, s.Valid)
	})

	t.Run("scan unsupported type returns error", func(t *testing.T) {
		var s NullableSSVC
		err := s.Scan(12345)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot scan type")
	})

	t.Run("scan invalid JSON returns error", func(t *testing.T) {
		var s NullableSSVC
		err := s.Scan(`{not json}`)
		assert.Error(t, err)
	})

	t.Run("round-trip through Value and Scan", func(t *testing.T) {
		original := NullableSSVC{
			SSVCScore: SSVCScore{
				Vector:      "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/",
				Decision:    "Scheduled",
				Methodology: "Supplier",
			},
			Valid: true,
		}
		val, err := original.Value()
		require.NoError(t, err)

		var restored NullableSSVC
		err = restored.Scan(val)
		require.NoError(t, err)
		assert.True(t, restored.Valid)
		assert.Equal(t, original.SSVCScore, restored.SSVCScore)
	})
}

func TestNullableSSVC_MarshalJSON(t *testing.T) {
	t.Run("valid SSVC marshals as inner object", func(t *testing.T) {
		s := NullableSSVC{
			SSVCScore: SSVCScore{
				Vector:      "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/",
				Decision:    "Immediate",
				Methodology: "Supplier",
			},
			Valid: true,
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		assert.JSONEq(t,
			`{"vector":"SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/","decision":"Immediate","methodology":"Supplier"}`,
			string(b),
		)
	})

	t.Run("invalid SSVC marshals as null", func(t *testing.T) {
		s := NullableSSVC{Valid: false}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		assert.Equal(t, "null", string(b))
	})

	t.Run("marshal output matches Value database encoding", func(t *testing.T) {
		s := NullableSSVC{
			SSVCScore: SSVCScore{
				Vector:      "SSVCv2/E:A",
				Decision:    "Defer",
				Methodology: "Supplier",
			},
			Valid: true,
		}
		jsonBytes, err := json.Marshal(s)
		require.NoError(t, err)
		dbVal, err := s.Value()
		require.NoError(t, err)
		assert.JSONEq(t, dbVal.(string), string(jsonBytes),
			"JSON marshaling and database Value() must produce equivalent encodings")
	})
}

func TestNullableSSVC_UnmarshalJSON(t *testing.T) {
	t.Run("JSON object sets valid", func(t *testing.T) {
		var s NullableSSVC
		err := json.Unmarshal(
			[]byte(`{"vector":"SSVCv2/E:A","decision":"Scheduled","methodology":"Supplier"}`), &s)
		require.NoError(t, err)
		assert.True(t, s.Valid)
		assert.Equal(t, "SSVCv2/E:A", s.Vector)
		assert.Equal(t, "Scheduled", s.Decision)
		assert.Equal(t, "Supplier", s.Methodology)
	})

	t.Run("JSON null sets invalid and clears inner value", func(t *testing.T) {
		s := NullableSSVC{
			SSVCScore: SSVCScore{Vector: "stale", Decision: "stale", Methodology: "stale"},
			Valid:     true,
		}
		err := json.Unmarshal([]byte(`null`), &s)
		require.NoError(t, err)
		assert.False(t, s.Valid)
		assert.Equal(t, SSVCScore{}, s.SSVCScore)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		var s NullableSSVC
		err := json.Unmarshal([]byte(`{not json}`), &s)
		assert.Error(t, err)
	})

	t.Run("round-trip through Marshal and Unmarshal", func(t *testing.T) {
		original := NullableSSVC{
			SSVCScore: SSVCScore{
				Vector:      "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/",
				Decision:    "Out-of-Cycle",
				Methodology: "Supplier",
			},
			Valid: true,
		}
		b, err := json.Marshal(original)
		require.NoError(t, err)

		var restored NullableSSVC
		err = json.Unmarshal(b, &restored)
		require.NoError(t, err)
		assert.Equal(t, original, restored)
	})

	t.Run("round-trip of invalid value", func(t *testing.T) {
		original := NullableSSVC{Valid: false}
		b, err := json.Marshal(original)
		require.NoError(t, err)

		var restored NullableSSVC
		err = json.Unmarshal(b, &restored)
		require.NoError(t, err)
		assert.Equal(t, original, restored)
	})
}
