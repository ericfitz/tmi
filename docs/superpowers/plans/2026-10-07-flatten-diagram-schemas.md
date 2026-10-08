# Flatten Diagram Schemas Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `Node`, `Edge`, `DfdDiagram` and `DfdDiagramInput` standalone schemas with polymorphism declared only at `oneOf` use sites, without changing the wire format (#956).

**Architecture:** Two new unit tests pin the contract first: a payload corpus that must validate before and after, and a structure test that fails on today's spec. Then the spec is rewritten with a jq script, `api/api.go` is regenerated, and the Go code is adapted to the regenerated types. Housekeeping and environment-level verification (integration, Postman, tmi-clients) follow.

**Tech Stack:** Go 1.x, Gin, oapi-codegen v2.7.1, kin-openapi (`github.com/getkin/kin-openapi/openapi3`), jq, testify.

**Spec:** `docs/superpowers/specs/2026-10-07-flatten-diagram-schemas-design.md`

## Global Constraints

- The JSON on the wire does not change: every payload valid today stays valid; nothing invalid today becomes valid except where the spec says.
- Every `shape` and `type` field in the diagram schemas uses maxLength 64.
- Never call generated `FromNode`, `MergeNode`, `FromMinimalNode`, `MergeMinimalNode` in non-generated code; use `SafeFromNode`/`SafeFromEdge` (`api/cell_union_helpers.go`). `make check-unsafe-union-methods` enforces this.
- Always use make targets (`make generate-api`, `make test-unit name=TestX count1=true`, `make lint`, `make verify`); never `go test`/`go run` directly.
- Logging only via `github.com/ericfitz/tmi/internal/slogging`.
- Use `rg` with an explicit path; use `jq` for `api-schema/tmi-openapi.json` (never Read it whole). Back up a file before editing it with jq (`cp f f.bak`), and delete the backup once the edit is confirmed, before committing.
- New or changed Go functions get a `// SEM@<sha>: <intent>` marker line directly above them (one line, ≤ ~12 words, canonical verb first). Use `SEM@0000000` as the sha placeholder in new code; the controller refreshes markers at the end.
- Conventional commits; end each commit message with:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01TwPH4r1SH2go1BFp6iQcGG`.
- Commit only; never push.

## Review Focus

1. A node in flat format (`x/y/width/height`) and one in nested format (`position/size`) both still validate and both still round-trip through `Node.UnmarshalJSON`/`MarshalJSON`: Task 1 corpus plus the existing `node_unmarshal` tests in Task 3.
2. Cell `data` with extra user keys beyond `_metadata`/`security_boundary`/`data_assets` still validates and survives a round-trip (additionalProperties true): Task 1 corpus and Task 3 round-trip test.
3. An off-enum `shape` (for example `threat-model-process`) or `type` (`DFD-2.0.0`) is rejected by the schema both before and after: Task 1 negative cases.
4. An edge missing `source` or `target` is rejected before and after: Task 1 negative cases.
5. A threat model GET response with embedded diagrams (`ThreatModel.diagrams`) validates and decodes into `DfdDiagram` after the `Diagram` wrapper is removed: Task 1 corpus plus Task 3 handler tests.

---

### Task 1: Diagram payload corpus test (passes before and after)

**Files:**
- Create: `api/testdata/diagram_payloads/valid/*.json`, `api/testdata/diagram_payloads/invalid/*.json`
- Create: `api/openapi_diagram_payloads_test.go`

**Interfaces:**
- Produces: `loadSpecFromFile(t *testing.T) *openapi3.T` (loads `../api-schema/tmi-openapi.json` with `openapi3.NewLoader()`, `IsExternalRefsAllowed=false`, then `doc.Validate(loader.Context)` must succeed). Task 2 reuses it.
- Corpus file naming: `<SchemaName>__<case>.json`, where `SchemaName` is the component schema to validate against (`DfdDiagram`, `DfdDiagramInput`, `ThreatModel`, `Node`, `Edge`).

- [ ] **Step 1: Freeze today's payloads into the corpus.** Write these files (pretty-printed JSON). Extract the spec examples with jq from the current spec, for example `jq '.components.schemas.DfdDiagram.example' api-schema/tmi-openapi.json > api/testdata/diagram_payloads/valid/DfdDiagram__spec_example.json`. If the example lives under an `allOf` element, take it from there.
  - `valid/DfdDiagram__spec_example.json`: the `DfdDiagram` example.
  - `valid/DfdDiagramInput__spec_example.json`: the `DfdDiagramInput` example.
  - `valid/ThreatModel__get_example.json`: the 200 example of GET `/threat_models/{threat_model_id}` (find it with `jq '.paths["/threat_models/{threat_model_id}"].get.responses["200"].content["application/json"]'`). If it embeds no diagram, add the `DfdDiagram` example into its `diagrams` array.
  - `valid/Node__patch_addcell.json`: the cell value of the PATCH `addCell` example on `/threat_models/{threat_model_id}/diagrams/{diagram_id}`.
  - `valid/DfdDiagram__mixed_cells.json`, written by hand: a full diagram (all required `BaseDiagram` fields: `id`, `name`, `type: "DFD-1.0.0"`, `created_at`, `modified_at`, `cells`) whose cells are: a `process` node in nested format (`position`, `size`); a `store` node in flat format (`x`, `y`, `width`, `height`); a `security-boundary` node with `children`; a `text-box` node; an `actor` node with `data` holding `_metadata` (array of `{key, value}`), `security_boundary`, `data_assets`, and an extra key `"custom_note": "kept"`; a `flow` edge with `source`/`target` objects, `labels`, `vertices`, `router`, `connector`. Use valid UUIDs everywhere ids are UUIDs. Check each property name against the current `Node`/`Edge`/`Cell` schemas with jq before writing it.
  - `valid/DfdDiagramInput__mixed_cells.json`: the same cells with only the input fields (`name`, `type`, `cells`, optional `description`).
  - `invalid/Node__off_enum_shape.json`: a node with `shape: "threat-model-process"`.
  - `invalid/DfdDiagramInput__bad_type.json`: an input with `type: "DFD-2.0.0"`.
  - `invalid/Edge__missing_target.json`: a flow edge without `target`.
  - `invalid/DfdDiagramInput__edge_missing_source.json`: an input whose only cell is a flow edge without `source`.

- [ ] **Step 2: Write the test.**

```go
package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

// SEM@0000000: load and validate the OpenAPI spec from the repository file (test helper)
func loadSpecFromFile(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join("..", "api-schema", "tmi-openapi.json"))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(loader.Context))
	return doc
}

// SEM@0000000: validate frozen diagram payloads against their schemas, valid and invalid (test)
func TestDiagramPayloadCorpus(t *testing.T) {
	doc := loadSpecFromFile(t)
	for _, dir := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("testdata", "diagram_payloads", dir, "*.json"))
		require.NoError(t, err)
		require.NotEmpty(t, files, "no corpus files in %s", dir)
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f), ".json")
			schemaName, _, ok := strings.Cut(name, "__")
			require.True(t, ok, "corpus file %s must be named <Schema>__<case>.json", f)
			t.Run(dir+"/"+name, func(t *testing.T) {
				ref, found := doc.Components.Schemas[schemaName]
				require.True(t, found, "schema %s not in spec", schemaName)
				raw, err := os.ReadFile(f)
				require.NoError(t, err)
				var v any
				require.NoError(t, json.Unmarshal(raw, &v))
				err = ref.Value.VisitJSON(v)
				if dir == "valid" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}
```

If kin-openapi needs `openapi3.EnableFormatValidation`-style options to reject an invalid uuid, don't add them; the test checks structure, not formats.

- [ ] **Step 3: Run it on today's spec.**
  Run: `make test-unit name=TestDiagramPayloadCorpus count1=true`
  Expected: PASS on every case. If a valid file fails, fix the corpus file (it must reflect what today's spec accepts), not the spec. If an invalid file passes, report it to the controller: that is a gap in today's spec, not something to fix here.

- [ ] **Step 4: Commit.**

```bash
git add api/openapi_diagram_payloads_test.go api/testdata/diagram_payloads
git commit -m "test(api): pin today's diagram payloads against the OpenAPI spec (#956)"
```

---

### Task 2: Structure test (fails on today's spec)

**Files:**
- Create: `api/openapi_schema_structure_test.go`

**Interfaces:**
- Consumes: none (reads the spec file as raw JSON, so it sees `allOf`/`$ref` structure exactly as written).
- Produces: `TestDiagramSchemaStructure`, which Task 3 turns green.

- [ ] **Step 1: Write the test.** It checks the diagram schemas only. The whole spec has two unrelated redeclarations (`User`, `ExtendedAsset`) that are out of scope.

```go
package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// diagramSchemaScope reports whether a component schema belongs to the diagram family (#956).
// SEM@0000000: decide whether a component schema is a diagram or cell schema (pure)
func diagramSchemaScope(name string) bool {
	return name == "Node" || name == "Edge" || strings.Contains(name, "Diagram") || strings.Contains(name, "Cell")
}

// SEM@0000000: resolve a local component $ref to its schema name (pure)
func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/components/schemas/")
}

// SEM@0000000: collect property names a schema declares, following allOf refs (pure)
func declaredProps(schemas map[string]map[string]any, name string, seen map[string]bool) map[string]bool {
	out := map[string]bool{}
	if seen[name] {
		return out
	}
	seen[name] = true
	s := schemas[name]
	if p, ok := s["properties"].(map[string]any); ok {
		for k := range p {
			out[k] = true
		}
	}
	if all, ok := s["allOf"].([]any); ok {
		for _, e := range all {
			em, _ := e.(map[string]any)
			if r, ok := em["$ref"].(string); ok {
				for k := range declaredProps(schemas, refName(r), seen) {
					out[k] = true
				}
			} else if p, ok := em["properties"].(map[string]any); ok {
				for k := range p {
					out[k] = true
				}
			}
		}
	}
	return out
}

// SEM@0000000: check diagram schemas for redeclared allOf props and misplaced discriminators (test)
func TestDiagramSchemaStructure(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "api-schema", "tmi-openapi.json"))
	require.NoError(t, err)
	var doc struct {
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	schemas := doc.Components.Schemas
	var problems []string

	for name, s := range schemas {
		if !diagramSchemaScope(name) {
			continue
		}
		all, _ := s["allOf"].([]any)
		base := map[string]bool{}
		var bases []string
		for _, e := range all {
			em, _ := e.(map[string]any)
			if r, ok := em["$ref"].(string); ok {
				bases = append(bases, refName(r))
				for k := range declaredProps(schemas, refName(r), map[string]bool{}) {
					base[k] = true
				}
			}
		}
		for _, e := range all {
			em, _ := e.(map[string]any)
			if _, isRef := em["$ref"]; isRef {
				continue
			}
			p, _ := em["properties"].(map[string]any)
			for k := range p {
				if base[k] {
					problems = append(problems, fmt.Sprintf("%s: allOf child redeclares %q from %v", name, k, bases))
				}
			}
		}
	}

	// Discriminators: anywhere under a diagram schema, only next to oneOf/anyOf,
	// and no mapping target may allOf the schema that hosts the discriminator.
	var walk func(host, path string, node any)
	walk = func(host, path string, node any) {
		switch v := node.(type) {
		case map[string]any:
			if d, ok := v["discriminator"].(map[string]any); ok {
				_, hasOneOf := v["oneOf"]
				_, hasAnyOf := v["anyOf"]
				if !hasOneOf && !hasAnyOf {
					problems = append(problems, fmt.Sprintf("%s: discriminator without oneOf/anyOf", path))
				}
				if m, ok := d["mapping"].(map[string]any); ok {
					for _, target := range m {
						ts, _ := target.(string)
						tAll, _ := schemas[refName(ts)]["allOf"].([]any)
						for _, e := range tAll {
							em, _ := e.(map[string]any)
							if r, _ := em["$ref"].(string); refName(r) == host {
								problems = append(problems, fmt.Sprintf("%s: mapping target %s is a subtype of %s", path, refName(ts), host))
							}
						}
					}
				}
			}
			for k, c := range v {
				walk(host, path+"."+k, c)
			}
		case []any:
			for i, c := range v {
				walk(host, fmt.Sprintf("%s[%d]", path, i), c)
			}
		}
	}
	for name, s := range schemas {
		if diagramSchemaScope(name) {
			walk(name, name, s)
		}
	}

	sort.Strings(problems)
	require.Empty(t, problems, "diagram schema structure violations:\n%s", strings.Join(problems, "\n"))
}
```

- [ ] **Step 2: Run it.**
  Run: `make test-unit name=TestDiagramSchemaStructure count1=true`
  Expected: FAIL, listing at least: `Node` and `Edge` redeclaring `shape`; `DfdDiagram` and `DfdDiagramInput` redeclaring `type`; `Cell`, `BaseDiagram`, `BaseDiagramInput` discriminators without `oneOf`; `BaseDiagram` mapping target `DfdDiagram` as a subtype of `BaseDiagram` (and the same for the input). Save the failure output in the task report (the "before" evidence).

- [ ] **Step 3: Commit the failing test on its own** so the before-state is recorded. `make verify` is red at this commit until Task 3 lands; say so in the commit body.

```bash
git add api/openapi_schema_structure_test.go
git commit -m "test(api): flag diagram schema allOf redeclarations and base discriminators (#956)

Fails on the current spec by design; the schema flattening in the next
commit makes it pass."
```

---

### Task 3: Flatten the schemas, regenerate, adapt the Go code

**Files:**
- Create: `scripts/flatten-diagram-schemas.jq` (one-off transform, committed so reviewers can audit it; Task 4 deletes it)
- Modify: `api-schema/tmi-openapi.json` (via the jq script)
- Regenerate: `api/api.go` (`make generate-api`)
- Modify: `api/node_unmarshal.go`, `api/cache_service.go` (delete `CacheCells`/`GetCachedCells` and anything only they use), `api/types.go` (delete `DiagramRequest`), `api/database_store_gorm.go`, `api/internal_models.go`, `api/threat_model_diagram_handlers.go`, `api/threat_model_handlers.go`, `api/websocket.go`, `api/test_fixtures.go`, `api/cell_union_helpers_test.go`, and any other file the compiler flags.

**Interfaces:**
- Consumes: `TestDiagramPayloadCorpus` (Task 1) and `TestDiagramSchemaStructure` (Task 2).
- Produces: new spec schemas `CellId`, `CellData`; generated Go type `CellData`; `ThreatModel.Diagrams *[]DfdDiagram`; deleted Go types `Cell`, `BaseDiagram`, `BaseDiagramInput`, `Diagram`, `Node_Data`, `Edge_Data`. `Node`, `Edge`, `DfdDiagram`, `DfdDiagramInput`, `DfdDiagram_Cells_Item`, `DfdDiagramInput_Cells_Item`, `Minimal*` keep their names.

- [ ] **Step 1: Write the jq transform** `scripts/flatten-diagram-schemas.jq`. Build every new schema from the existing definitions so no constraint is retyped by hand:
  - `CellId` = `.components.schemas.Cell.properties.id`; `CellData` = `.components.schemas.Cell.properties.data`.
  - `Node` = `{type: "object", description: <Node's current description, or the allOf child's>, required: ["id","shape"], properties: ({id: {"$ref": "#/components/schemas/CellId"}, data: {"$ref": "#/components/schemas/CellData"}} + <Node.allOf[1].properties>)}` with `properties.shape` = Node's own shape (enum), plus `pattern` copied from `Cell.properties.shape.pattern`, `maxLength: 64`, keeping its description. Keep `example` and any other keywords from both the Node wrapper and its allOf child.
  - `Edge` the same way, with `required: ["id","shape","source","target"]` (Cell's required + the child's required, deduplicated).
  - `DfdDiagram` = `{type: "object", required: (BaseDiagram.required + child.required | unique-preserving-order), properties: (BaseDiagram.properties + child.properties + {version: <top-level DfdDiagram.properties.version>})}` with `properties.type` = `BaseDiagram.properties.type` but `maxLength: 64` and the child's description if it has one. Carry over `description`, `example`. Drop `x-discriminator-value`.
  - `DfdDiagramInput` the same from `BaseDiagramInput` (keep `minLength: 1` on `type`, set `maxLength: 64`).
  - `ThreatModel.allOf[1].properties.diagrams.items` = `{"$ref": "#/components/schemas/DfdDiagram"}`; keep any `description` on `diagrams`.
  - `DiagramListItem.properties.type.maxLength` = 64.
  - Delete `Cell`, `BaseDiagram`, `BaseDiagramInput`, `Diagram`.
  - Finally, assert no `$ref` to a deleted schema remains: `[.. | objects | .["$ref"]? // empty | select(test("/(Cell|BaseDiagram|BaseDiagramInput|Diagram)$"))] | if length > 0 then error("dangling refs: \(.)") else . end` applied to the whole document.
  Apply it: `cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.bak && jq -f scripts/flatten-diagram-schemas.jq api-schema/tmi-openapi.json.bak > api-schema/tmi-openapi.json`. Keep the JSON formatting consistent with the file (2-space indent, as `jq` emits); check `git diff --stat` shows changes only in the schemas above (examples elsewhere must not reformat; if jq reformats unrelated lines, stop and report).

- [ ] **Step 2: Check the spec.**
  Run: `make validate-openapi` — expected: no new errors compared with `main` (compare the error count in `api-schema/openapi-validation-report.json` against a run on the `.bak` if needed).
  Run: `make test-unit name=TestDiagramSchemaStructure count1=true` — expected: PASS.
  Run: `make test-unit name=TestDiagramPayloadCorpus count1=true` — expected: PASS (valid cases pass, invalid cases fail validation).
  Delete the `.bak` once both pass.

- [ ] **Step 3: Regenerate.** Run `make generate-api`. Confirm `oapi-codegen` is v2.7.1 (`oapi-codegen -version`); if not, stop and report. Then `rg -n 'func \(t \*DfdDiagram_Cells_Item\) FromNode' -A 12 api/api.go` and record whether `FromNode` sets `Shape` to a fixed value.

- [ ] **Step 4: Fix compilation.** Run `make build-server` and fix each error:
  - `Node_Data`/`Edge_Data` → `CellData` (including `api/node_unmarshal.go` temp structs at lines ~38 and ~118, and `api/cell_union_helpers_test.go`).
  - `Diagram` wrapper → `DfdDiagram`. Where code did `d.AsDfdDiagram()` on a `Diagram`, use the `DfdDiagram` value directly; where it did `var d Diagram; d.FromDfdDiagram(x)`, use `x`. `models.Diagram` (GORM, `api/models`) is unrelated: don't touch it.
  - Delete `CacheCells`/`GetCachedCells` (`api/cache_service.go`) and `DiagramRequest` (`api/types.go`) and their tests, if any. Confirm with `rg -n 'CacheCells|GetCachedCells|DiagramRequest' .` that nothing else used them.
  - Comments that mention `BaseDiagram`/`Diagram` wrapper (`api/websocket.go:318`, `api/threat_model_diagram_handlers_test.go:946-950`, `test/integration/workflows/diagram_crud_test.go:154,217`, `cmd/dbtool/seedspec_diagram.go:23`): update them to the new names.
  Repeat until `make build-server` succeeds, then `make lint` (which includes `check-unsafe-union-methods`).

- [ ] **Step 5: Round-trip test for `CellData` extra keys.** Add to `api/cell_union_helpers_test.go`:

```go
// SEM@0000000: verify cell data extra keys survive a Node JSON round-trip (test)
func TestNodeCellDataRoundTripKeepsExtraKeys(t *testing.T) {
	in := []byte(`{"id":"6f1c2c8e-7a51-4a38-9b1e-0d1c8f5a2b10","shape":"process","x":10,"y":20,"width":120,"height":60,"data":{"custom_note":"kept","_metadata":[{"key":"k","value":"v"}]}}`)
	var n Node
	require.NoError(t, json.Unmarshal(in, &n))
	out, err := json.Marshal(n)
	require.NoError(t, err)
	var back map[string]any
	require.NoError(t, json.Unmarshal(out, &back))
	data, ok := back["data"].(map[string]any)
	require.True(t, ok, "data missing after round-trip: %s", out)
	require.Equal(t, "kept", data["custom_note"])
	require.NotNil(t, data["_metadata"])
}
```

  Run: `make test-unit name=TestNodeCellDataRoundTripKeepsExtraKeys count1=true` — expected PASS. If it fails, fix `node_unmarshal.go` (it must carry `CellData` through, including its `AdditionalProperties`), not the test. Run it against `main`'s code too (`git stash` is banned: use `git worktree add /private/tmp/claude-501/-Users-efitz-Projects-tmi/301df58d-3168-4af6-9604-74bad3ce1221/scratchpad/wt-main main`, copy the test in, run `make test-unit name=... count1=true` there, then `git worktree remove` it) and record whether it passed before.

- [ ] **Step 6: Full unit suite and gate.**
  Run: `make verify`
  Expected: `verify: OK`. Fix failures at the root (tests asserting old type names update to new ones; never skip a test).

- [ ] **Step 7: Commit.**

```bash
git add api-schema/tmi-openapi.json api/ scripts/flatten-diagram-schemas.jq test/integration/workflows/diagram_crud_test.go cmd/dbtool/seedspec_diagram.go
git commit -m "feat(api)!: flatten diagram cell and diagram schemas

Node, Edge, DfdDiagram and DfdDiagramInput are standalone objects; the
shape and type discriminators live only on the cells oneOf. Cell,
BaseDiagram, BaseDiagramInput and the deprecated Diagram wrapper are
removed (ThreatModel.diagrams items are DfdDiagram). Wire format is
unchanged; generated client type names change.

Refs #956"
```

  Report to the controller: the list of files changed, the `FromNode` finding from Step 3, the round-trip before/after result, and the last lines of `make verify`.

---

### Task 4: Housekeeping (tombstone script, vacuum ruleset, 3.1 copy, union-method guidance)

**Files:**
- Modify: `scripts/add-tombstone-openapi.jq` (the `BaseDiagram` line → `DfdDiagram`; update its comment)
- Modify: `vacuum-ruleset.yaml` (lines ~97-107, ~123-127, ~152)
- Delete: `api-schema/tmi-openapi-3.1-experimental.json`, `scripts/flatten-diagram-schemas.jq` (one-off transform from Task 3)
- Modify (only if Task 3 found that `FromNode` no longer hardcodes `shape`): `api/cell_union_helpers.go` header comment, `scripts/check-unsafe-union-methods.py` docstring, `.claude/CLAUDE.md` "Discriminator union type safety" paragraph

- [ ] **Step 1: Tombstone script.** Change `.components.schemas.BaseDiagram.properties.deleted_at = deleted_at_prop` to `.components.schemas.DfdDiagram.properties.deleted_at = deleted_at_prop` and its comment to `# Add deleted_at to DfdDiagram`. Prove it is idempotent on the new spec: `jq -f scripts/add-tombstone-openapi.jq api-schema/tmi-openapi.json | jq -S '.components.schemas.DfdDiagram.properties.deleted_at' ` must equal `jq -S '.components.schemas.DfdDiagram.properties.deleted_at' api-schema/tmi-openapi.json` (the property already exists after Task 3, carried over from `BaseDiagram`).

- [ ] **Step 2: Vacuum ruleset.** Read `vacuum-ruleset.yaml` lines 90-160. Remove exemptions whose only targets were `Cell`, `BaseDiagram`, `BaseDiagramInput` or `Diagram`; narrow mixed ones by removing just those targets. Run `make validate-openapi`: expected no new errors or warnings compared with Task 3's run. If removing an exemption surfaces a finding on the new flat schemas, keep that exemption narrowed to the new schema and note why in a YAML comment.

- [ ] **Step 3: Delete** `api-schema/tmi-openapi-3.1-experimental.json`. Run `rg -n 'tmi-openapi-3.1-experimental' .` and update or remove any remaining mention (docs under `docs/superpowers/` are history: leave them).

- [ ] **Step 4: Union-method guidance.** If Task 3 reported that the regenerated `FromNode`/`MergeNode`/`FromMinimalNode`/`MergeMinimalNode` do not hardcode `shape`, rewrite the guidance to say what is true: the generated methods marshal the value as given, `SafeFromNode`/`SafeFromEdge` remain the required entry points as a guard against regressions in oapi-codegen, and the lint check stays. Keep the CLAUDE.md paragraph's rule ("never call ... in non-generated code") and fix only the stated reason. If `FromNode` does hardcode `shape`, change nothing here.

- [ ] **Step 5: Gate and commit.**
  Run: `make verify` — expected `verify: OK`.

```bash
git add scripts/add-tombstone-openapi.jq vacuum-ruleset.yaml api/cell_union_helpers.go scripts/check-unsafe-union-methods.py .claude/CLAUDE.md
git rm api-schema/tmi-openapi-3.1-experimental.json scripts/flatten-diagram-schemas.jq
git commit -m "chore(api): retarget tombstone script and vacuum exemptions to flat diagram schemas (#956)"
```

---

### Task 5: Environment verification (controller runs this; not a subagent task)

**Files:**
- Create: `api/testdata/diagram_payloads/valid/DfdDiagram__dev_server_get.json`, `valid/ThreatModel__dev_server_get.json` (captured responses, ids kept, no tokens)

- [ ] **Step 1:** `make test-integration` (self-contained PostgreSQL container on 5433; does not touch the dev DB). Expected: all pass.
- [ ] **Step 2:** Post on general/tmi before using docker-desktop; then deploy the branch build to the dev stack and run `make test-api` (Postman). Check whether the factory's off-enum shapes (`threat-model-process`, `threat-model-datastore`) fail on `main` as well; if they do, file a separate bug.
- [ ] **Step 3:** Capture a real GET diagram and GET threat model (with a diagram that has nodes and an edge) from the dev server as `alice`, save them into the corpus, run `make test-unit name=TestDiagramPayloadCorpus count1=true`, commit `test(api): add dev-server diagram payloads to the corpus (#956)`.
- [ ] **Step 4:** In `/Users/efitz/Projects/tmi-clients`, regenerate Python, Go and TS from this branch's spec with that repo's documented regeneration commands, decode the captured payloads with each client (cells must resolve to Node/Edge), and confirm the patch logs report nothing to fix. Do not commit or push in tmi-clients; report results and restore its working tree.
- [ ] **Step 5:** Oracle review (`oracle-db-admin` agent) of the `api/database_store_gorm.go` change; refresh SEM markers (`/sem-annotate --update` on changed Go files); `graphify update .`; final `make verify`; whole-branch review.
