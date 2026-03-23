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

1. **Evaluate ogen** as the next candidate. Per issue #180's analysis, ogen has its own limitations (no Gin integration, no JSON Patch, several open bugs), but it may handle discriminated unions and `unevaluatedProperties` differently. A focused evaluation of just these two criteria would quickly determine viability.

2. **Investigate runtime validation as a separate concern**. Instead of requiring the code generator to enforce `unevaluatedProperties`, consider a two-tool approach: any code generator for types and routing + a standalone 3.1-capable validator (e.g., pb33f/libopenapi-validator) in middleware for runtime request validation. This decouples the migration decision from a single tool's completeness.

3. **Consider contributing to oapi-codegen/kin-openapi**. The upstream projects need funding for 3.1 support. If both ogen and openapi-generator fail the evaluation, contributing to the existing toolchain may be more practical than migrating.

4. **Keep the 3.1 spec**. The converted spec (`api-schema/tmi-openapi-3.1.json`) is valid and useful regardless of which generator is eventually chosen.
