# Evaluate openapi-generator as oapi-codegen Replacement

**Date**: 2026-03-23
**Issue**: #180 (deps: evaluate migration from oapi-codegen to another tool for OpenAPI code generation)
**Blocks**: #87 (deps: migrate to OpenAPI 3.1+)

## Problem

TMI needs to migrate its OpenAPI specification from 3.0.3 to 3.1 to use `unevaluatedProperties: false` with `allOf` composition, eliminating current workarounds. The current code generator, oapi-codegen, has no path to 3.1 support (blocked on kin-openapi parser, both projects need funding). This evaluation determines whether openapi-generator is a viable replacement.

## Goal

Reach a well-informed go/no-go decision on migrating from oapi-codegen to openapi-generator by:
1. Converting the OpenAPI spec from 3.0.3 to 3.1
2. Running openapi-generator's `go-gin-server` against the 3.1 spec
3. Analyzing generated output against TMI's requirements
4. Prototyping Gin middleware integration
5. Documenting findings with a decision

## Current State

- **Generated code**: `api/api.go` (19,629 lines) — models, Gin server handlers, embedded spec
- **ServerInterface**: 283 methods
- **Handler implementations**: ~308 Server methods across 136 Go files using Gin
- **Middleware stack**: 15+ Gin middleware files (JWT, CORS, rate limiting, resource authorization, etc.)
- **OpenAPI validation middleware**: `api/openapi_middleware.go` imports `kin-openapi` and `oapi-codegen/gin-middleware` for runtime request validation — a hard dependency on oapi-codegen's ecosystem
- **OpenAPI spec**: ~1.9 MB, uses allOf (71 instances), discriminators (5), JSON Patch (12 endpoints)
- **Discriminator workarounds**: `cell_union_helpers.go` works around oapi-codegen's broken discriminator handling

## Constraints

- Handler signatures may change; the team is open to adapting them
- Gin middleware stack must remain functional (investment in 15+ middleware files)
- WebSocket support is handled outside the code generator (not a blocker)
- The evaluation targets a 3.1 version of the spec, not the current 3.0.3
- openapi-generator requires a JVM; Docker-based invocation is acceptable for both local dev and CI/CD

## Design

### Phase 1: OpenAPI 3.0.3 to 3.1 Spec Conversion

**Output**: `api-schema/tmi-openapi-3.1.json` (new file; `tmi-openapi.json` remains unchanged)

Key transformations:

| 3.0.3 Pattern | 3.1 Equivalent |
|---|---|
| `"openapi": "3.0.3"` | `"openapi": "3.1.0"` |
| `"nullable": true` + `"type": "string"` | `"type": ["string", "null"]` |
| `"nullable": true` without explicit type | Add `"null"` to type array |
| `exclusiveMinimum: true` + `minimum: N` | `exclusiveMinimum: N` (JSON Schema 2020-12) |
| No `jsonSchemaDialect` | Add `"jsonSchemaDialect": "https://json-schema.org/draft/2020-12/schema"` |
| `example` (on schemas) | Keep as-is; `example` is deprecated in 3.1 but still valid. Migrate to `examples` (array) only if needed later. |

Unchanged: All paths, operations, security schemes, discriminators, allOf composition, vendor extensions (`x-public-endpoint`, `x-cacheable-endpoint`), and JSON Patch content types.

**Validation**: Run Vacuum (which supports 3.1) after conversion. Acceptance threshold: zero errors; warnings reviewed and documented.

### Phase 2: Code Generation with openapi-generator

**Tool**: `openapi-generator-cli` (latest stable v7.x)
**Primary generator**: `go-gin-server`
**Fallback generator**: `go-server` (stdlib `net/http`) — test if `go-gin-server` produces unusable output, since `go-server` with a Gin adapter may still be viable
**Input**: `api-schema/tmi-openapi-3.1.json`
**Output**: `eval/openapi-generator-output/` (isolated directory)

Evaluation targets:

1. **Models** — allOf composition (correct field merging?), discriminated unions (especially multi-value node shape mapping), nullable types (proper Go representation for `["string", "null"]`), JSON Patch request bodies (recognized or skipped?)
2. **Server stubs** — handler function signatures (compatible with Gin middleware?), path parameter patterns (TMI uses unique names like `threat_model_id`, `asset_id` — no duplicates), multiple content types per operation (JSON + JSON Patch)
3. **Compilation** — does the generated code compile as-is?
4. **`unevaluatedProperties` enforcement** — add `unevaluatedProperties: false` to 2-3 allOf composed schemas in the 3.1 spec, regenerate, and verify the generated Go code enforces the constraint (rejects unknown properties). This is the primary reason for the 3.1 migration.

### Phase 3: Middleware Integration Prototype

**Goal**: Prove TMI's Gin middleware stack works with openapi-generator's routing.

**Scope**: JWT authentication middleware applied to one generated endpoint. JWT is the most pervasive middleware (touches all non-public endpoints) and reads/writes `gin.Context` (user identity, auth headers). If this works, other middleware (CORS, rate limiting, resource authorization) follows the same `gin.HandlerFunc` pattern.

Steps:
1. Take one generated endpoint handler (e.g., `GetThreatModels`)
2. Wire TMI's existing JWT middleware into the generated router
3. Verify compilation and correct middleware execution order
4. Document any adapter code needed

**Output**: `eval/middleware-prototype/` package importing generated code and TMI's auth middleware.

**Not prototyped** (documented only):
- WebSocket integration (outside generator scope, handled separately)
- Resource-specific authorization middleware (same `gin.HandlerFunc` pattern as JWT)

**Risk: Runtime request validation middleware**

The current `openapi_middleware.go` depends on `kin-openapi` and `oapi-codegen/gin-middleware` for runtime OpenAPI request/response validation. This is a hard dependency on oapi-codegen's ecosystem, not just another `gin.HandlerFunc`. The evaluation must document how runtime validation would work post-migration. Options include:
- openapi-generator provides its own validation layer
- A standalone 3.1-capable validation library replaces kin-openapi (e.g., pb33f/libopenapi-validator)
- kin-openapi is retained solely for validation (partial migration)
- Runtime validation is replaced by generated validation code

### Phase 4: Go/No-Go Decision

Three possible outcomes:
1. **Go** — meets requirements; create detailed migration plan
2. **Go with caveats** — works but needs specific workarounds; document each and its cost
3. **No-go** — blocking issues; recommend next steps (evaluate ogen, wait, contribute upstream)

Decision criteria in priority order:

| Criteria | Go | No-Go |
|---|---|---|
| allOf composition | Correct field merging, compiles | Broken structs or missing fields |
| Discriminated unions | Usable (even with workarounds) | Non-compilable or semantically wrong |
| JSON Patch endpoints | Generated (even as generic handler) | Silently dropped from router |
| Gin middleware compat | Standard `gin.HandlerFunc` chain works | Requires forking generator or abandoning Gin |
| Runtime request validation | Viable path exists (any of the options above) | No 3.1-capable validation option available |
| Nullable types | Proper Go representation | Lost or always required |
| Code quality | Idiomatic enough to maintain | Excessive boilerplate or unreadable |
| `unevaluatedProperties` | Generated code enforces the constraint | Parsed but ignored (defeats purpose of migration) |

A single No-Go on the first five criteria blocks the migration. The last three are weighted — multiple weak results together could also block.

## Deliverables

| Artifact | Location | Purpose |
|---|---|---|
| 3.1 OpenAPI spec | `api-schema/tmi-openapi-3.1.json` | Standalone value; used for all future 3.1 work |
| Generated code | `eval/openapi-generator-output/` | Raw generator output for inspection |
| Middleware prototype | `eval/middleware-prototype/` | Proves Gin middleware integration |
| Evaluation report | Appended to this document | Findings, go/no-go decision, next steps |

The 3.1 spec and this document are committed to the repo. The `eval/` directory is committed for reference.

## After the Decision

- **If Go**: Create a phased migration plan covering spec switch, generator integration, handler adaptation, middleware wiring, runtime validation replacement, test updates, and performance benchmarking.
- **If No-Go**: Update issue #180 with findings. Evaluate ogen as fallback. Update issue #87 with revised status.

---

## Evaluation Report

**Date**: 2026-03-23
**openapi-generator version**: 7.20.0
**Generators tested**: `go-gin-server`, `go-server` (fallback)
**Input spec**: `api-schema/tmi-openapi-3.1.json` (converted from 3.0.3)

### Decision: No-Go

openapi-generator is not a viable replacement for oapi-codegen for the TMI codebase. Both Go generators (`go-gin-server` and `go-server`) share the same fundamental limitations that produce blocking failures.

### Spec Conversion (Phase 1): Success

The OpenAPI 3.0.3 to 3.1 conversion was clean:
- 145 nullable fields converted to type arrays
- Zero errors from Vacuum validation
- All paths (150), schemas (166), discriminators (7), and vendor extensions preserved
- Conversion script created at `scripts/convert-openapi-3.0-to-3.1.sh`

The 3.1 spec (`api-schema/tmi-openapi-3.1.json`) has standalone value regardless of the generator decision.

### Decision Matrix

| Criteria | go-gin-server | go-server | Required |
|---|---|---|---|
| allOf composition | PARTIAL PASS — discriminator+allOf drops parent fields | PASS — uses struct embedding | Blocking |
| Discriminated unions | **FAIL** — flat-merges Node+Edge into one 21-field struct | **FAIL** — identical flat-merge | Blocking |
| JSON Patch endpoints | PASS — all 26 routes generated | PASS | Blocking |
| Gin middleware compat | PASS — `gin.HandlerFunc` compatible | N/A (uses net/http) | Blocking |
| Runtime request validation | Not tested (skipped due to unevaluatedProperties failure) | Not tested | Blocking |
| Nullable types | PARTIAL PASS — objects lose pointer, allOf degrades to `*interface{}` | Similar | Weighted |
| Code quality | FAIL — duplicate enum constants, no typed params, inline schema explosion | FAIL — same enum issue + duplicate import bug | Weighted |
| `unevaluatedProperties` | **FAIL** — silently stripped from spec | **FAIL** — identical behavior | Weighted (but critical for migration purpose) |

### Blocking Failures (detail)

**1. `unevaluatedProperties: false` silently ignored (both generators)**

This is the entire reason for the 3.1 migration (#87). When `unevaluatedProperties: false` is added to allOf schemas, the generator:
- Strips the keyword from its internal representation
- Produces byte-for-byte identical output
- Generates zero validation code
- Does not even log a warning

The keyword is not in openapi-generator's Go template schema model. This failure alone makes openapi-generator unsuitable for TMI's migration goal.

**2. Discriminated unions flat-merged (both generators)**

The `oneOf: [Node, Edge]` with `shape` discriminator in `DfdDiagram.cells` is rendered as a single flat struct combining all 21 fields from both types. No type-safe dispatch, no union interface, no discriminator-based marshaling. Node-specific `NodeAttrs` is replaced by `EdgeAttrs` in the merged type. `MinimalCell` has the same problem.

This is worse than oapi-codegen's broken discriminator handling, which at least preserves separate `Node` and `Edge` types (requiring only the `SafeFromNode`/`SafeFromEdge` workaround in `cell_union_helpers.go`).

**3. Duplicate enum constants prevent compilation (both generators)**

Both generators emit unqualified package-level constants for enum values. When `TeamStatus` and `ProjectStatus` share values (`active`, `on_hold`, `archived`), and `TeamMemberRole` and `RelationshipType` share `other`, the Go compiler rejects the duplicate identifiers. The `go-server` generator additionally has a duplicate import bug triggered by large specs.

### Notable Positives

- **Gin middleware integration** (`go-gin-server`): Handler signatures are `func(c *gin.Context)` — fully compatible with TMI's 15+ middleware files. `router.Use()` before `NewRouterWithGinEngine()` works for global middleware. Zero middleware changes needed.
- **JSON Patch support**: All 26 PATCH routes generated with correct `JsonPatchDocumentInner` model per RFC 6902.
- **allOf struct embedding** (`go-server`): Uses Go struct embedding for inheritance — DfdDiagram correctly embeds BaseDiagram, Node embeds Cell. This is better than `go-gin-server` which dropped parent fields.
- **3.1 parsing**: The generator successfully parses OpenAPI 3.1 (though marked as "beta").

### Recommended Next Steps

See ogen evaluation below.

---

## Evaluation Report: ogen v1.20.1

**Date**: 2026-03-23
**ogen version**: v1.20.1
**Spec tested**: Focused test spec (TMI's full 3.0.3 spec failed on `ErrorHeaders` name conflict; 3.1 spec failed on type array parsing)

### Decision: Go with caveats

ogen is the strongest candidate evaluated, with excellent discriminated union handling that directly solves TMI's most painful code generation problem. However, adoption requires accepting trade-offs on unevaluatedProperties and the Gin framework.

### Decision Matrix

| Criteria | ogen | openapi-generator (for comparison) |
|---|---|---|
| allOf composition | **PASS** — flat merged structs, fields correct | PARTIAL PASS / FAIL |
| Discriminated unions | **PASS** — type-safe sum types with per-value constructors | FAIL — flat-merged |
| JSON Patch endpoints | **PASS** — `application/json-patch+json` handled correctly | PASS |
| Gin middleware compat | **FAIL** — generates `net/http`, not Gin | PASS |
| unevaluatedProperties | **FAIL** — silently ignored | FAIL |
| Nullable types | **PASS** — `OptNilT` wrapper types (3.0.3 `nullable: true` only) | PARTIAL PASS |
| Code quality | **PASS** — compiles cleanly, type-safe, built-in validation | FAIL |
| Type arrays `["string","null"]` | **FAIL** — cannot parse (ogen #1617) | PASS |

### Key Findings

**1. Discriminated unions are excellent (PASS)**

This is ogen's standout feature and directly solves TMI's `SafeFromNode()`/`SafeFromEdge()` workaround:
- Generates a `Cell` sum type with `CellType` discriminator
- Each discriminator value gets its own constant and constructor: `NewCellProcessCell(v Node)`, `NewCellDataStoreCell(v Node)`, etc.
- JSON encoder preserves the correct shape value per variant
- JSON decoder dispatches on the `shape` field to the correct struct
- No manual helper code needed — the shape is preserved through the type system

**2. unevaluatedProperties not supported (FAIL)**

Same as openapi-generator — silently ignored. No Go code generator currently supports this JSON Schema 2020-12 keyword. This means `unevaluatedProperties` enforcement must come from a separate runtime validator, not the code generator.

**3. Type arrays cannot be parsed (FAIL, ogen #1617)**

ogen cannot handle `type: ["string", "null"]`, the standard 3.1 nullable syntax. The parser expects `type` to be a scalar string. The 3.1 spec's 145 type-array instances would all need to be expressed as `nullable: true` (3.0.3 style) or `oneOf: [{type: "string"}, {type: "null"}]` instead. This is a downgrade from pure 3.1 idioms but doesn't block functionality.

**4. Full TMI spec has a name conflict (BLOCKED on `ErrorHeaders`)**

The full TMI 3.0.3 spec fails with `anonymous type name conflict: "ErrorHeaders"`. This occurs because multiple operations return error responses with identical rate-limit headers, and ogen generates the same anonymous type name for each. Workaround: name the anonymous response schema explicitly in the spec.

**5. JSON Patch works (PASS)**

Contrary to issue #180's analysis (which cited ogen issue #1587), JSON Patch content type is handled correctly in v1.20.1. The request decoder checks for `application/json-patch+json` and deserializes as `[]JsonPatchOp`.

**6. No Gin integration (known limitation)**

ogen generates standalone `net/http` handlers. TMI's 15+ Gin middleware files would need to be rewritten as `net/http` middleware. This is significant effort but is a one-time migration — the middleware logic itself doesn't change, only the framework types.

### Migration Path (if proceeding)

A two-tool approach is recommended:

1. **ogen** for code generation — types, routing, request/response encoding, discriminated unions, basic validation
2. **pb33f/libopenapi-validator** (or similar) for runtime `unevaluatedProperties` enforcement — runs as middleware before handlers

Spec adjustments needed:
- Convert type arrays back to `nullable: true` for ogen compatibility (or wait for ogen #1617 fix)
- Name the anonymous `ErrorHeaders` response schema to avoid name conflict
- Add `x-ogen-operation-group` vendor extensions for logical handler grouping

Framework migration:
- Rewrite Gin middleware as `net/http` middleware (same logic, different types)
- Mount WebSocket handler separately on the HTTP mux (already outside code generator scope)
- ogen's built-in OpenTelemetry replaces TMI's manual instrumentation (#150)

### Follow-up: Full TMI Spec Test with ogen

After fixing the `ErrorHeaders` conflict (replacing 531 inline error responses with `$ref` to component responses), ogen successfully generated code from the full TMI 3.0.3 spec.

**Results:**
- **Generation: SUCCESS** — required `ignore_not_implemented: ["all"]` config for 3 unsupported features (discriminator inference on JSON Patch bodies, `application/samlmetadata+xml`, object defaults)
- **Compilation: SUCCESS** — zero errors, 20 Go files, ~682K lines
- **265 of 281 operations generated** (94%) — 16 skipped
- **Discriminated unions: Excellent** — `DfdDiagramCellsItem` is a proper sum type with all 6 shape values (`actor`, `process`, `security-boundary`, `store`, `text-box`, `flow`). Includes `IsNode()`/`IsEdge()` methods and typed setters. Directly solves the `SafeFromNode`/`SafeFromEdge` workaround.
- **Handler interface: 265 typed methods** — each with typed request/response parameters and context. `UnimplementedHandler` struct provided for progressive adoption.

**16 skipped operations:**
- **15 JSON Patch endpoints** — ogen cannot handle the oneOf discriminator in the JSON Patch request body schema. Affects: `patchThreatModel`, `patchThreatModelThreat`, `patchThreatModelDiagram`, `patchThreatModelDocument`, `patchThreatModelNote`, `patchThreatModelRepository`, `patchThreatModelAsset`, `patchAdminSurvey`, `patchIntakeSurveyResponse`, `patchTriageSurveyResponse`, `PatchProject`, `PatchTeam`, `bulkPatchThreatModelThreats`, and 2 bulk metadata operations.
- **1 SAML endpoint** — `getSAMLMetadata` uses unsupported `application/samlmetadata+xml` content type.

**Workaround for JSON Patch:** Define the JSON Patch schema as a simple array of objects without oneOf discrimination on the `op` field, or handle these 15 endpoints outside the generated code (manual route registration with the same middleware chain).

### Follow-up: pb33f/libopenapi-validator Evaluation

**`unevaluatedProperties: false` enforcement: CONFIRMED WORKING.**

Empirical testing with a 3.1 spec containing an allOf-composed schema with `unevaluatedProperties: false`:
- Request with only known properties (`name`, `description`): **PASSED validation**
- Request with extra unknown property (`extra_field`): **REJECTED** with validation error
- Required field missing: **REJECTED** ("missing property 'name'")
- Wrong type: **REJECTED** ("got number, want string")
- Empty body: **REJECTED** ("request body is empty")

**Library capabilities:**
- Full OpenAPI 3.1 + JSON Schema 2020-12 support (including `unevaluatedProperties`)
- `ValidateHttpRequestSync(*http.Request)` for per-request validation
- Built-in schema cache + radix tree for O(k) path matching (suitable for TMI's 1.9 MB spec)
- Very actively maintained: 5 releases in the last month (latest v0.13.3, 2026-03-15)
- No built-in middleware, but Gin integration is ~20 lines

**Gin middleware pattern:**
```go
func OpenAPIValidationMiddleware(v validator.Validator) gin.HandlerFunc {
    return func(c *gin.Context) {
        valid, errs := v.ValidateHttpRequestSync(c.Request)
        if !valid {
            c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
                "error":   "request validation failed",
                "details": formatValidationErrors(errs),
            })
            return
        }
        c.Next()
    }
}
```

Note: If migrating to ogen (which uses `net/http`), pb33f still works directly since it takes `*http.Request`.

### Updated Recommendation

The two-tool approach is now validated on both sides:

1. **ogen** for code generation — type-safe discriminated unions, 265/281 operations, compiled successfully
2. **pb33f/libopenapi-validator** for runtime validation — `unevaluatedProperties` enforcement confirmed, full HTTP request validation, high performance

**Spec preparation needed:**
- Replace 531 inline error responses with `$ref` to component responses (already tested, reduces spec from 1.9 MB to 1.35 MB)
- Use `nullable: true` instead of type arrays for ogen compatibility (or wait for ogen #1617)
- Simplify JSON Patch schema to avoid oneOf discriminator inference (or handle 15 PATCH endpoints outside generated code)
- Add `x-ogen-operation-group` vendor extensions for handler grouping
- Add ogen config file with `ignore_not_implemented: ["all"]`

**Framework decision:**
- **Option A**: Migrate from Gin to `net/http` (ogen's native output). Rewrite 15+ middleware files. ogen's built-in OpenTelemetry replaces #150.
- **Option B**: Keep Gin, wrap ogen's `http.Handler` with `gin.WrapH()`. Mixed middleware approach — more complex but smaller migration.
- **Option C**: Keep Gin + oapi-codegen for now, add pb33f/libopenapi-validator as middleware for `unevaluatedProperties` enforcement. Smallest change, but doesn't get ogen's discriminator improvements.

**Next steps:**
1. Decide on framework approach (A, B, or C)
2. If A or B: Create detailed migration plan with phased approach
3. If C: Integrate pb33f/libopenapi-validator into the existing middleware stack
4. Monitor ogen #1617 (type arrays) — when fixed, migrate spec to pure 3.1
