package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patchCase is one JSON Patch scenario against a store-backed entity. Either
// wantErr (a 400 RequestError and an unchanged row) or check (the patch was
// applied, verified by a follow-up Get) must hold; a silent no-op satisfies
// neither because every check asserts a changed value.
type patchCase[T any] struct {
	name    string
	ops     []PatchOperation
	wantErr bool
	check   func(t *testing.T, got *T)
}

// patchHarness gives runPatchCases a fresh seeded entity per case.
type patchHarness[T any] struct {
	patch func(ops []PatchOperation) error
	get   func() *T
}

func runPatchCases[T any](t *testing.T, seed func(t *testing.T) patchHarness[T], cases []patchCase[T]) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := seed(t)
			before := h.get()
			err := h.patch(tc.ops)
			if tc.wantErr {
				require.Error(t, err)
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "want *RequestError, got %T: %v", err, err)
				assert.Equal(t, http.StatusBadRequest, reqErr.Status, reqErr.Message)
				after := h.get()
				assert.Equal(t, before, after, "a rejected patch must not change the row")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, tc.check, "success case needs a check")
			tc.check(t, h.get())
		})
	}
}

func pop(op, path string, value any) PatchOperation {
	return PatchOperation{Op: op, Path: path, Value: value}
}

// ---------------------------------------------------------------- threats

func seedThreat(t *testing.T) patchHarness[Threat] {
	db, tm := setupThreatAliasTestDB(t)
	repo := NewGormThreatRepository(db, nil, nil)
	ctx := context.Background()
	tmID := uuid.MustParse(string(tm.ID))
	score := float32(5)
	cwe := []string{"CWE-79"}
	md := []Metadata{{Key: "k1", Value: "v1"}}
	seedAsset, seedDiagram, seedCell := uuid.New(), uuid.New(), uuid.New()
	threat := &Threat{
		AssetId:         &seedAsset,
		DiagramId:       &seedDiagram,
		CellId:          &seedCell,
		Ssvc:            &SSVCScore{Vector: "v", Decision: Scheduled, Methodology: "coordinator"},
		Name:            "orig",
		Description:     strPtr("d"),
		Severity:        strPtr("high"),
		Status:          strPtr("open"),
		Priority:        strPtr("low"),
		Mitigation:      strPtr("m"),
		IssueUri:        strPtr("https://example.com/1"),
		Mitigated:       boolP(false),
		Score:           &score,
		CweId:           &cwe,
		ThreatType:      []string{"spoofing"},
		IncludeInReport: boolP(true),
		TimmyEnabled:    boolP(true),
		Metadata:        &md,
		ThreatModelId:   &tmID,
	}
	require.NoError(t, repo.Create(ctx, threat))
	id := threat.Id.String()
	return patchHarness[Threat]{
		patch: func(ops []PatchOperation) error {
			_, err := repo.Patch(ctx, string(tm.ID), id, ops)
			return err
		},
		get: func() *Threat {
			g, err := repo.Get(ctx, id)
			require.NoError(t, err)
			return g
		},
	}
}

func TestThreatPatch_GenericApplier(t *testing.T) {
	asset := uuid.New().String()
	diagram := uuid.New().String()
	cell := uuid.New().String()
	cases := []patchCase[Threat]{
		{name: "add /name (reported bug)", ops: []PatchOperation{pop("add", "/name", "new")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "new", g.Name) }},
		{name: "replace /name", ops: []PatchOperation{pop("replace", "/name", "new")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "new", g.Name) }},
		{name: "remove /name", ops: []PatchOperation{{Op: "remove", Path: "/name"}}, wantErr: true},
		{name: "empty name", ops: []PatchOperation{pop("replace", "/name", "")}, wantErr: true},
		{name: "whitespace name via add", ops: []PatchOperation{pop("add", "/name", "  ")}, wantErr: true},
		{name: "non-string name", ops: []PatchOperation{pop("add", "/name", 5)}, wantErr: true},

		{name: "add /description", ops: []PatchOperation{pop("add", "/description", "x")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "x", *g.Description) }},
		{name: "replace /description", ops: []PatchOperation{pop("replace", "/description", "x")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "x", *g.Description) }},
		{name: "remove /description", ops: []PatchOperation{{Op: "remove", Path: "/description"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Description) }},

		{name: "add /severity", ops: []PatchOperation{pop("add", "/severity", "low")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "low", *g.Severity) }},
		{name: "replace /severity", ops: []PatchOperation{pop("replace", "/severity", "low")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "low", *g.Severity) }},
		{name: "remove /severity", ops: []PatchOperation{{Op: "remove", Path: "/severity"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Severity) }},

		{name: "add /status", ops: []PatchOperation{pop("add", "/status", "closed")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "closed", *g.Status) }},
		{name: "replace /status", ops: []PatchOperation{pop("replace", "/status", "closed")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "closed", *g.Status) }},
		{name: "remove /status", ops: []PatchOperation{{Op: "remove", Path: "/status"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Status) }},

		{name: "add /priority", ops: []PatchOperation{pop("add", "/priority", "high")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "high", *g.Priority) }},
		{name: "replace /priority", ops: []PatchOperation{pop("replace", "/priority", "high")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "high", *g.Priority) }},
		{name: "remove /priority", ops: []PatchOperation{{Op: "remove", Path: "/priority"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Priority) }},

		{name: "add /mitigation", ops: []PatchOperation{pop("add", "/mitigation", "fix")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "fix", *g.Mitigation) }},
		{name: "replace /mitigation", ops: []PatchOperation{pop("replace", "/mitigation", "fix")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "fix", *g.Mitigation) }},
		{name: "remove /mitigation", ops: []PatchOperation{{Op: "remove", Path: "/mitigation"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Mitigation) }},

		{name: "add /mitigated", ops: []PatchOperation{pop("add", "/mitigated", true)},
			check: func(t *testing.T, g *Threat) { assert.True(t, *g.Mitigated) }},
		{name: "replace /mitigated", ops: []PatchOperation{pop("replace", "/mitigated", true)},
			check: func(t *testing.T, g *Threat) { assert.True(t, *g.Mitigated) }},
		{name: "non-bool mitigated", ops: []PatchOperation{pop("add", "/mitigated", "yes")}, wantErr: true},

		{name: "add /score", ops: []PatchOperation{pop("add", "/score", 7.5)},
			check: func(t *testing.T, g *Threat) { assert.InDelta(t, 7.5, float64(*g.Score), 0.01) }},
		{name: "replace /score", ops: []PatchOperation{pop("replace", "/score", 7.5)},
			check: func(t *testing.T, g *Threat) { assert.InDelta(t, 7.5, float64(*g.Score), 0.01) }},
		{name: "remove /score", ops: []PatchOperation{{Op: "remove", Path: "/score"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Score) }},

		{name: "add /issue_uri", ops: []PatchOperation{pop("add", "/issue_uri", "https://example.com/2")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "https://example.com/2", *g.IssueUri) }},
		{name: "replace /issue_uri", ops: []PatchOperation{pop("replace", "/issue_uri", "https://example.com/2")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "https://example.com/2", *g.IssueUri) }},
		{name: "remove /issue_uri", ops: []PatchOperation{{Op: "remove", Path: "/issue_uri"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.IssueUri) }},

		{name: "add /cwe_id", ops: []PatchOperation{pop("add", "/cwe_id", []any{"CWE-1", "CWE-2"})},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, []string{"CWE-1", "CWE-2"}, *g.CweId) }},
		{name: "replace /cwe_id", ops: []PatchOperation{pop("replace", "/cwe_id", []any{"CWE-3"})},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, []string{"CWE-3"}, *g.CweId) }},
		{name: "remove /cwe_id", ops: []PatchOperation{{Op: "remove", Path: "/cwe_id"}},
			check: func(t *testing.T, g *Threat) { assert.Empty(t, derefSlice(g.CweId)) }},

		{name: "add /cvss", ops: []PatchOperation{pop("add", "/cvss", []any{map[string]any{"vector": "CVSS:3.1/AV:N", "score": 9.8}})},
			check: func(t *testing.T, g *Threat) {
				require.NotNil(t, g.Cvss)
				require.Len(t, *g.Cvss, 1)
				assert.Equal(t, "CVSS:3.1/AV:N", (*g.Cvss)[0].Vector)
			}},
		{name: "replace /cvss (unset field)", ops: []PatchOperation{pop("replace", "/cvss", []any{map[string]any{"vector": "CVSS:3.1/AV:L", "score": 5.0}})},
			check: func(t *testing.T, g *Threat) { require.Len(t, *g.Cvss, 1) }},
		{name: "remove /cvss on unset field", ops: []PatchOperation{{Op: "remove", Path: "/cvss"}}, wantErr: true},

		{name: "add /ssvc", ops: []PatchOperation{pop("add", "/ssvc", map[string]any{"vector": "SSVCv2/E:A/", "decision": "Act", "methodology": "supplier"})},
			check: func(t *testing.T, g *Threat) {
				require.NotNil(t, g.Ssvc)
				assert.Equal(t, "supplier", g.Ssvc.Methodology)
			}},
		{name: "replace /ssvc (unset field)", ops: []PatchOperation{pop("replace", "/ssvc", map[string]any{"vector": "v", "decision": "Track", "methodology": "deployer"})},
			check: func(t *testing.T, g *Threat) { require.NotNil(t, g.Ssvc) }},

		{name: "add /asset_id", ops: []PatchOperation{pop("add", "/asset_id", asset)},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, asset, g.AssetId.String()) }},
		{name: "replace /asset_id (unset field)", ops: []PatchOperation{pop("replace", "/asset_id", asset)},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, asset, g.AssetId.String()) }},
		{name: "add /diagram_id", ops: []PatchOperation{pop("add", "/diagram_id", diagram)},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, diagram, g.DiagramId.String()) }},
		{name: "add /cell_id", ops: []PatchOperation{pop("add", "/cell_id", cell)},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, cell, g.CellId.String()) }},
		{name: "invalid uuid asset_id", ops: []PatchOperation{pop("add", "/asset_id", "nope")}, wantErr: true},

		{name: "add /include_in_report", ops: []PatchOperation{pop("add", "/include_in_report", false)},
			check: func(t *testing.T, g *Threat) { assert.False(t, *g.IncludeInReport) }},
		{name: "replace /timmy_enabled", ops: []PatchOperation{pop("replace", "/timmy_enabled", false)},
			check: func(t *testing.T, g *Threat) { assert.False(t, *g.TimmyEnabled) }},
		{name: "add /timmy_enabled", ops: []PatchOperation{pop("add", "/timmy_enabled", false)},
			check: func(t *testing.T, g *Threat) { assert.False(t, *g.TimmyEnabled) }},
		{name: "remove /include_in_report restores default true", ops: []PatchOperation{pop("add", "/include_in_report", false), {Op: "remove", Path: "/include_in_report"}},
			check: func(t *testing.T, g *Threat) { assert.True(t, *g.IncludeInReport) }},

		{name: "replace /threat_type", ops: []PatchOperation{pop("replace", "/threat_type", []any{"tampering", "repudiation"})},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, []string{"tampering", "repudiation"}, g.ThreatType) }},
		{name: "add /threat_type array", ops: []PatchOperation{pop("add", "/threat_type", []any{"dos"})},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, []string{"dos"}, g.ThreatType) }},
		{name: "add /threat_type/-", ops: []PatchOperation{pop("add", "/threat_type/-", "dos")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, []string{"spoofing", "dos"}, g.ThreatType) }},
		{name: "remove /threat_type/0", ops: []PatchOperation{{Op: "remove", Path: "/threat_type/0"}},
			check: func(t *testing.T, g *Threat) { assert.Empty(t, g.ThreatType) }},
		{name: "add /threat_type bare string", ops: []PatchOperation{pop("add", "/threat_type", "dos")}, wantErr: true},

		{name: "add /metadata", ops: []PatchOperation{pop("add", "/metadata", []any{map[string]any{"key": "k2", "value": "v2"}})},
			check: func(t *testing.T, g *Threat) {
				require.Len(t, *g.Metadata, 1)
				assert.Equal(t, "k2", (*g.Metadata)[0].Key)
			}},
		{name: "replace /metadata", ops: []PatchOperation{pop("replace", "/metadata", []any{map[string]any{"key": "k3", "value": "v3"}})},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "k3", (*g.Metadata)[0].Key) }},
		{name: "remove /metadata clears", ops: []PatchOperation{{Op: "remove", Path: "/metadata"}},
			check: func(t *testing.T, g *Threat) { assert.Empty(t, derefSlice(g.Metadata)) }},
		{name: "empty metadata value", ops: []PatchOperation{pop("add", "/metadata", []any{map[string]any{"key": "k", "value": ""}})}, wantErr: true},
		{name: "oversize metadata value", ops: []PatchOperation{pop("add", "/metadata", []any{map[string]any{"key": "k", "value": strings.Repeat("v", 1025)}})}, wantErr: true},
		{name: "score out of range", ops: []PatchOperation{pop("add", "/score", 100)}, wantErr: true},
		{name: "bad metadata key", ops: []PatchOperation{pop("add", "/metadata", []any{map[string]any{"key": "bad key!", "value": "v"}})}, wantErr: true},

		{name: "copy into /name rejected", ops: []PatchOperation{{Op: "copy", From: "/description", Path: "/name"}}, wantErr: true},
		{name: "move into /name rejected", ops: []PatchOperation{{Op: "move", From: "/description", Path: "/name"}}, wantErr: true},
		{name: "copy into /description rejected", ops: []PatchOperation{{Op: "copy", From: "/name", Path: "/description"}}, wantErr: true},
		{name: "copy into /mitigation rejected", ops: []PatchOperation{{Op: "copy", From: "/description", Path: "/mitigation"}}, wantErr: true},
		{name: "copy into /issue_uri rejected", ops: []PatchOperation{{Op: "copy", From: "/description", Path: "/issue_uri"}}, wantErr: true},
		{name: "move into /issue_uri rejected", ops: []PatchOperation{{Op: "move", From: "/description", Path: "/issue_uri"}}, wantErr: true},
		{name: "remove /asset_id", ops: []PatchOperation{{Op: "remove", Path: "/asset_id"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.AssetId) }},
		{name: "remove /diagram_id", ops: []PatchOperation{{Op: "remove", Path: "/diagram_id"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.DiagramId) }},
		{name: "remove /cell_id", ops: []PatchOperation{{Op: "remove", Path: "/cell_id"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.CellId) }},
		{name: "remove /ssvc", ops: []PatchOperation{{Op: "remove", Path: "/ssvc"}},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Ssvc) }},
		{name: "add /name null", ops: []PatchOperation{pop("add", "/name", nil)}, wantErr: true},
		{name: "add /description null", ops: []PatchOperation{pop("add", "/description", nil)},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Description) }},
		{name: "add /severity null", ops: []PatchOperation{pop("add", "/severity", nil)},
			check: func(t *testing.T, g *Threat) { assert.Nil(t, g.Severity) }},
		{name: "add /metadata null clears", ops: []PatchOperation{pop("add", "/metadata", nil)},
			check: func(t *testing.T, g *Threat) { assert.Empty(t, derefSlice(g.Metadata)) }},
		{name: "add /threat_type null", ops: []PatchOperation{pop("add", "/threat_type", nil)},
			check: func(t *testing.T, g *Threat) { assert.Empty(t, g.ThreatType) }},
		{name: "unrelated patch keeps metadata", ops: []PatchOperation{pop("add", "/name", "n2")},
			check: func(t *testing.T, g *Threat) {
				assert.Equal(t, "n2", g.Name)
				require.Len(t, *g.Metadata, 1)
				assert.Equal(t, "v1", (*g.Metadata)[0].Value)
			}},
		{name: "nested /metadata/0/value", ops: []PatchOperation{pop("replace", "/metadata/0/value", "v9")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "v9", (*g.Metadata)[0].Value) }},
		{name: "nested /metadata/0/value markup is sanitized or rejected", ops: []PatchOperation{pop("replace", "/metadata/0/value", "<script>alert(1)</script>ok")},
			check: func(t *testing.T, g *Threat) { assert.NotContains(t, (*g.Metadata)[0].Value, "<script") }},
		{name: "nested /metadata/0/value empty", ops: []PatchOperation{pop("replace", "/metadata/0/value", "")}, wantErr: true},
		{name: "nested /metadata/0/key invalid", ops: []PatchOperation{pop("replace", "/metadata/0/key", "bad key!")}, wantErr: true},
		{name: "trailing-slash path /ssvc/", ops: []PatchOperation{pop("add", "/ssvc/", map[string]any{})}, wantErr: true},
		{name: "empty-segment path", ops: []PatchOperation{pop("add", "/threat_type//0", "x")}, wantErr: true},
		{name: "root path", ops: []PatchOperation{pop("add", "", map[string]any{"name": "x"})}, wantErr: true},
		{name: "copy from /threat_model_id", ops: []PatchOperation{{Op: "copy", From: "/threat_model_id", Path: "/severity"}}, wantErr: true},
		{name: "copy from root", ops: []PatchOperation{{Op: "copy", From: "", Path: "/severity"}}, wantErr: true},
		{name: "test /threat_model_id", ops: []PatchOperation{pop("test", "/threat_model_id", uuid.New().String())}, wantErr: true},
		{name: "test ok then add", ops: []PatchOperation{pop("test", "/name", "orig"), pop("add", "/name", "n2")},
			check: func(t *testing.T, g *Threat) { assert.Equal(t, "n2", g.Name) }},
		{name: "test mismatch", ops: []PatchOperation{pop("test", "/name", "other"), pop("add", "/name", "n2")}, wantErr: true},
		{name: "unknown path", ops: []PatchOperation{pop("add", "/bogus", "x")}, wantErr: true},
		{name: "case-aliased path", ops: []PatchOperation{pop("add", "/Name", "x")}, wantErr: true},
		{name: "unknown op", ops: []PatchOperation{pop("frobnicate", "/name", "x")}, wantErr: true},
		{name: "read-only /id", ops: []PatchOperation{pop("replace", "/id", uuid.New().String())}, wantErr: true},
		{name: "read-only /threat_model_id", ops: []PatchOperation{pop("replace", "/threat_model_id", uuid.New().String())}, wantErr: true},
		{name: "read-only /alias", ops: []PatchOperation{pop("replace", "/alias", 99)}, wantErr: true},
		{name: "read-only /created_at", ops: []PatchOperation{pop("replace", "/created_at", "2020-01-01T00:00:00Z")}, wantErr: true},
		{name: "read-only /auto_generated", ops: []PatchOperation{pop("add", "/auto_generated", true)}, wantErr: true},
		{name: "read-only /version", ops: []PatchOperation{pop("replace", "/version", 7)}, wantErr: true},
		{name: "move from read-only", ops: []PatchOperation{{Op: "move", From: "/id", Path: "/description"}}, wantErr: true},
	}
	runPatchCases(t, seedThreat, cases)
}

func derefSlice[E any](p *[]E) []E {
	if p == nil {
		return nil
	}
	return *p
}

// ----------------------------------------------------------------- assets

func seedAsset(t *testing.T) patchHarness[Asset] {
	db, _, tm := setupAssetTestDB(t)
	repo := NewGormAssetRepository(db, nil, nil)
	ctx := context.Background()
	id := uuid.New()
	class := []string{"pii"}
	require.NoError(t, repo.Create(ctx, &Asset{
		Id: &id, Name: "orig", Type: AssetTypeSoftware, Description: strPtr("d"),
		Criticality: strPtr("high"), Sensitivity: strPtr("low"), Classification: &class,
		IncludeInReport: boolP(true), TimmyEnabled: boolP(true),
	}, string(tm.ID)))
	return patchHarness[Asset]{
		patch: func(ops []PatchOperation) error {
			_, err := repo.Patch(ctx, id.String(), ops)
			return err
		},
		get: func() *Asset {
			g, err := repo.Get(ctx, id.String())
			require.NoError(t, err)
			return g
		},
	}
}

func TestAssetPatch_GenericApplier(t *testing.T) {
	cases := []patchCase[Asset]{
		{name: "add /name (reported bug)", ops: []PatchOperation{pop("add", "/name", "new")},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, "new", g.Name) }},
		{name: "replace /name", ops: []PatchOperation{pop("replace", "/name", "new")},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, "new", g.Name) }},
		{name: "remove /name", ops: []PatchOperation{{Op: "remove", Path: "/name"}}, wantErr: true},
		{name: "empty name", ops: []PatchOperation{pop("replace", "/name", "")}, wantErr: true},
		{name: "add /type", ops: []PatchOperation{pop("add", "/type", "hardware")},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, AssetTypeHardware, g.Type) }},
		{name: "replace /type", ops: []PatchOperation{pop("replace", "/type", "hardware")},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, AssetTypeHardware, g.Type) }},
		{name: "remove /type", ops: []PatchOperation{{Op: "remove", Path: "/type"}}, wantErr: true},
		{name: "invalid /type", ops: []PatchOperation{pop("add", "/type", "bogus")}, wantErr: true},
		{name: "add /description", ops: []PatchOperation{pop("add", "/description", "x")},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, "x", *g.Description) }},
		{name: "remove /description", ops: []PatchOperation{{Op: "remove", Path: "/description"}},
			check: func(t *testing.T, g *Asset) { assert.Nil(t, g.Description) }},
		{name: "add /criticality", ops: []PatchOperation{pop("add", "/criticality", "low")},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, "low", *g.Criticality) }},
		{name: "replace /criticality", ops: []PatchOperation{pop("replace", "/criticality", "low")},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, "low", *g.Criticality) }},
		{name: "remove /criticality", ops: []PatchOperation{{Op: "remove", Path: "/criticality"}},
			check: func(t *testing.T, g *Asset) { assert.Nil(t, g.Criticality) }},
		{name: "add /sensitivity", ops: []PatchOperation{pop("add", "/sensitivity", "high")},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, "high", *g.Sensitivity) }},
		{name: "remove /sensitivity", ops: []PatchOperation{{Op: "remove", Path: "/sensitivity"}},
			check: func(t *testing.T, g *Asset) { assert.Nil(t, g.Sensitivity) }},
		{name: "add /classification", ops: []PatchOperation{pop("add", "/classification", []any{"a", "b"})},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, []string{"a", "b"}, *g.Classification) }},
		{name: "replace /classification", ops: []PatchOperation{pop("replace", "/classification", []any{"c"})},
			check: func(t *testing.T, g *Asset) { assert.Equal(t, []string{"c"}, *g.Classification) }},
		{name: "remove /classification", ops: []PatchOperation{{Op: "remove", Path: "/classification"}},
			check: func(t *testing.T, g *Asset) { assert.Empty(t, derefSlice(g.Classification)) }},
		{name: "add /include_in_report", ops: []PatchOperation{pop("add", "/include_in_report", false)},
			check: func(t *testing.T, g *Asset) { assert.False(t, *g.IncludeInReport) }},
		{name: "replace /timmy_enabled", ops: []PatchOperation{pop("replace", "/timmy_enabled", false)},
			check: func(t *testing.T, g *Asset) { assert.False(t, *g.TimmyEnabled) }},
		{name: "remove /timmy_enabled restores default true", ops: []PatchOperation{{Op: "remove", Path: "/timmy_enabled"}},
			check: func(t *testing.T, g *Asset) { assert.True(t, *g.TimmyEnabled) }},
		{name: "metadata not patchable", ops: []PatchOperation{pop("add", "/metadata", []any{})}, wantErr: true},
		{name: "copy into /name rejected", ops: []PatchOperation{{Op: "copy", From: "/description", Path: "/name"}}, wantErr: true},
		{name: "move into /name rejected", ops: []PatchOperation{{Op: "move", From: "/description", Path: "/name"}}, wantErr: true},
		{name: "copy into /description rejected", ops: []PatchOperation{{Op: "copy", From: "/name", Path: "/description"}}, wantErr: true},
		{name: "test mismatch", ops: []PatchOperation{pop("test", "/name", "other"), pop("add", "/name", "n2")}, wantErr: true},
		{name: "unknown path", ops: []PatchOperation{pop("add", "/bogus", "x")}, wantErr: true},
		{name: "read-only /id", ops: []PatchOperation{pop("replace", "/id", uuid.New().String())}, wantErr: true},
		{name: "read-only /alias", ops: []PatchOperation{pop("replace", "/alias", 9)}, wantErr: true},
	}
	runPatchCases(t, seedAsset, cases)
}

// -------------------------------------------------------------- documents

func seedDocument(t *testing.T) patchHarness[Document] {
	db, _, tm := setupDocumentAliasTestDB(t)
	repo := NewGormDocumentRepository(db, nil, nil)
	ctx := context.Background()
	id := uuid.New()
	require.NoError(t, repo.Create(ctx, &Document{
		Id: &id, Name: "orig", Uri: "https://example.com/a", Description: strPtr("d"),
		IncludeInReport: boolP(true), TimmyEnabled: boolP(true),
	}, string(tm.ID)))
	return patchHarness[Document]{
		patch: func(ops []PatchOperation) error {
			_, err := repo.Patch(ctx, id.String(), ops)
			return err
		},
		get: func() *Document {
			g, err := repo.Get(ctx, id.String())
			require.NoError(t, err)
			return g
		},
	}
}

func TestDocumentPatch_GenericApplier(t *testing.T) {
	cases := []patchCase[Document]{
		{name: "add /name (reported bug)", ops: []PatchOperation{pop("add", "/name", "new")},
			check: func(t *testing.T, g *Document) { assert.Equal(t, "new", g.Name) }},
		{name: "replace /name", ops: []PatchOperation{pop("replace", "/name", "new")},
			check: func(t *testing.T, g *Document) { assert.Equal(t, "new", g.Name) }},
		{name: "remove /name", ops: []PatchOperation{{Op: "remove", Path: "/name"}}, wantErr: true},
		{name: "empty name", ops: []PatchOperation{pop("replace", "/name", "")}, wantErr: true},
		{name: "add /uri", ops: []PatchOperation{pop("add", "/uri", "https://example.com/b")},
			check: func(t *testing.T, g *Document) { assert.Equal(t, "https://example.com/b", g.Uri) }},
		{name: "replace /uri", ops: []PatchOperation{pop("replace", "/uri", "https://example.com/b")},
			check: func(t *testing.T, g *Document) { assert.Equal(t, "https://example.com/b", g.Uri) }},
		{name: "remove /uri", ops: []PatchOperation{{Op: "remove", Path: "/uri"}}, wantErr: true},
		{name: "empty uri", ops: []PatchOperation{pop("replace", "/uri", "")}, wantErr: true},
		{name: "add /description", ops: []PatchOperation{pop("add", "/description", "x")},
			check: func(t *testing.T, g *Document) { assert.Equal(t, "x", *g.Description) }},
		{name: "remove /description", ops: []PatchOperation{{Op: "remove", Path: "/description"}},
			check: func(t *testing.T, g *Document) { assert.Nil(t, g.Description) }},
		{name: "add /include_in_report", ops: []PatchOperation{pop("add", "/include_in_report", false)},
			check: func(t *testing.T, g *Document) { assert.False(t, *g.IncludeInReport) }},
		{name: "replace /timmy_enabled", ops: []PatchOperation{pop("replace", "/timmy_enabled", false)},
			check: func(t *testing.T, g *Document) { assert.False(t, *g.TimmyEnabled) }},
		{name: "remove /timmy_enabled restores default true", ops: []PatchOperation{{Op: "remove", Path: "/timmy_enabled"}},
			check: func(t *testing.T, g *Document) { assert.True(t, *g.TimmyEnabled) }},
		{name: "metadata not patchable", ops: []PatchOperation{pop("add", "/metadata", []any{})}, wantErr: true},
		{name: "copy into /name rejected", ops: []PatchOperation{{Op: "copy", From: "/description", Path: "/name"}}, wantErr: true},
		{name: "move into /name rejected", ops: []PatchOperation{{Op: "move", From: "/description", Path: "/name"}}, wantErr: true},
		{name: "copy into /description rejected", ops: []PatchOperation{{Op: "copy", From: "/name", Path: "/description"}}, wantErr: true},
		{name: "copy into /uri rejected", ops: []PatchOperation{{Op: "copy", From: "/description", Path: "/uri"}}, wantErr: true},
		{name: "move into /uri rejected", ops: []PatchOperation{{Op: "move", From: "/description", Path: "/uri"}}, wantErr: true},
		{name: "test mismatch", ops: []PatchOperation{pop("test", "/name", "other"), pop("add", "/name", "n2")}, wantErr: true},
		{name: "unknown path", ops: []PatchOperation{pop("add", "/bogus", "x")}, wantErr: true},
		{name: "read-only /access_status", ops: []PatchOperation{pop("add", "/access_status", "accessible")}, wantErr: true},
		{name: "read-only /id", ops: []PatchOperation{pop("replace", "/id", uuid.New().String())}, wantErr: true},
	}
	runPatchCases(t, seedDocument, cases)
}

// ----------------------------------------------------------- repositories

func seedRepository(t *testing.T) patchHarness[Repository] {
	db, _, tm := setupRepositoryTestDB(t)
	repo := NewGormRepositoryRepository(db, nil, nil)
	ctx := context.Background()
	id := uuid.New()
	rt := RepositoryTypeGit
	require.NoError(t, repo.Create(ctx, &Repository{
		Id: &id, Name: strPtr("orig"), Uri: "https://github.com/example/repo", Type: &rt,
		Description: strPtr("d"), IncludeInReport: boolP(true), TimmyEnabled: boolP(true),
	}, string(tm.ID)))
	return patchHarness[Repository]{
		patch: func(ops []PatchOperation) error {
			_, err := repo.Patch(ctx, id.String(), ops)
			return err
		},
		get: func() *Repository {
			g, err := repo.Get(ctx, id.String())
			require.NoError(t, err)
			return g
		},
	}
}

func TestRepositoryPatch_GenericApplier(t *testing.T) {
	params := map[string]any{"refType": "branch", "refValue": "main", "subPath": "src"}
	cases := []patchCase[Repository]{
		{name: "add /name (reported bug)", ops: []PatchOperation{pop("add", "/name", "new")},
			check: func(t *testing.T, g *Repository) { assert.Equal(t, "new", *g.Name) }},
		{name: "replace /name", ops: []PatchOperation{pop("replace", "/name", "new")},
			check: func(t *testing.T, g *Repository) { assert.Equal(t, "new", *g.Name) }},
		{name: "remove /name (optional)", ops: []PatchOperation{{Op: "remove", Path: "/name"}},
			check: func(t *testing.T, g *Repository) { assert.Nil(t, g.Name) }},
		{name: "add /type", ops: []PatchOperation{pop("add", "/type", "svn")},
			check: func(t *testing.T, g *Repository) { assert.Equal(t, RepositoryTypeSvn, *g.Type) }},
		{name: "replace /type", ops: []PatchOperation{pop("replace", "/type", "svn")},
			check: func(t *testing.T, g *Repository) { assert.Equal(t, RepositoryTypeSvn, *g.Type) }},
		{name: "remove /type", ops: []PatchOperation{{Op: "remove", Path: "/type"}},
			check: func(t *testing.T, g *Repository) { assert.Nil(t, g.Type) }},
		{name: "invalid /type", ops: []PatchOperation{pop("add", "/type", "bogus")}, wantErr: true},
		{name: "add /uri", ops: []PatchOperation{pop("add", "/uri", "https://github.com/example/other")},
			check: func(t *testing.T, g *Repository) { assert.Equal(t, "https://github.com/example/other", g.Uri) }},
		{name: "replace /uri", ops: []PatchOperation{pop("replace", "/uri", "https://github.com/example/other")},
			check: func(t *testing.T, g *Repository) { assert.Equal(t, "https://github.com/example/other", g.Uri) }},
		{name: "remove /uri", ops: []PatchOperation{{Op: "remove", Path: "/uri"}}, wantErr: true},
		{name: "empty uri", ops: []PatchOperation{pop("replace", "/uri", "")}, wantErr: true},
		{name: "add /description", ops: []PatchOperation{pop("add", "/description", "x")},
			check: func(t *testing.T, g *Repository) { assert.Equal(t, "x", *g.Description) }},
		{name: "remove /description", ops: []PatchOperation{{Op: "remove", Path: "/description"}},
			check: func(t *testing.T, g *Repository) { assert.Nil(t, g.Description) }},
		{name: "add /parameters", ops: []PatchOperation{pop("add", "/parameters", params)},
			check: func(t *testing.T, g *Repository) {
				require.NotNil(t, g.Parameters)
				assert.Equal(t, "main", g.Parameters.RefValue)
			}},
		{name: "replace /parameters (unset field)", ops: []PatchOperation{pop("replace", "/parameters", params)},
			check: func(t *testing.T, g *Repository) { require.NotNil(t, g.Parameters) }},
		{name: "invalid /parameters refType", ops: []PatchOperation{pop("add", "/parameters", map[string]any{"refType": "bogus", "refValue": "x"})}, wantErr: true},
		{name: "add /include_in_report", ops: []PatchOperation{pop("add", "/include_in_report", false)},
			check: func(t *testing.T, g *Repository) { assert.False(t, *g.IncludeInReport) }},
		{name: "replace /timmy_enabled", ops: []PatchOperation{pop("replace", "/timmy_enabled", false)},
			check: func(t *testing.T, g *Repository) { assert.False(t, *g.TimmyEnabled) }},
		{name: "remove /timmy_enabled restores default true", ops: []PatchOperation{{Op: "remove", Path: "/timmy_enabled"}},
			check: func(t *testing.T, g *Repository) { assert.True(t, *g.TimmyEnabled) }},
		{name: "metadata not patchable", ops: []PatchOperation{pop("add", "/metadata", []any{})}, wantErr: true},
		{name: "copy into /name rejected", ops: []PatchOperation{{Op: "copy", From: "/description", Path: "/name"}}, wantErr: true},
		{name: "move into /name rejected", ops: []PatchOperation{{Op: "move", From: "/description", Path: "/name"}}, wantErr: true},
		{name: "copy into /description rejected", ops: []PatchOperation{{Op: "copy", From: "/name", Path: "/description"}}, wantErr: true},
		{name: "copy into /uri rejected", ops: []PatchOperation{{Op: "copy", From: "/description", Path: "/uri"}}, wantErr: true},
		{name: "move into /uri rejected", ops: []PatchOperation{{Op: "move", From: "/description", Path: "/uri"}}, wantErr: true},
		{name: "test mismatch", ops: []PatchOperation{pop("test", "/uri", "other"), pop("add", "/name", "n2")}, wantErr: true},
		{name: "unknown path", ops: []PatchOperation{pop("add", "/bogus", "x")}, wantErr: true},
		{name: "read-only /id", ops: []PatchOperation{pop("replace", "/id", uuid.New().String())}, wantErr: true},
	}
	runPatchCases(t, seedRepository, cases)
}

func TestApplyEntityPatch_MissingValidationConfigFailsClosed(t *testing.T) {
	_, err := applyEntityPatch(Threat{Name: "x"}, []PatchOperation{pop("add", "/name", "y")},
		"threat", threatPatchablePaths, "no_such_validation_config")
	require.Error(t, err, "a missing validation config must reject, not skip every validator")
	assert.Contains(t, err.Error(), "no_such_validation_config")
}
