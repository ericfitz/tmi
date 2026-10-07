# Flatten diagram cell and diagram schemas (#956)

Status: approved (Eric, 2026-10-07). Branch `feature/956-flatten-diagram-schemas`.

## Goal

The diagram schemas in `api-schema/tmi-openapi.json` describe polymorphism twice and narrow
inherited properties by redeclaring them in `allOf` children. Code generators turn that into
inheritance or embedding with shadowed fields, which broke the Go client (tmi-clients#55) and
the Python client (tmi-clients#44). Restructure the schemas so each concrete type stands alone
and polymorphism is declared only where it is used. The JSON on the wire does not change.

## Human decisions (Eric, 2026-10-07)

- The change is released as breaking for the schema: PR title `feat(api)!: ...`, so the
  version bot bumps the OpenAPI schema version MAJOR. Generated client type names disappear
  (`Cell`, `BaseDiagram`, `BaseDiagramInput`, `Diagram`), even though the wire format is
  unchanged.
- Client verification is in scope, done locally: regenerate Python, Go and TS in the local
  tmi-clients checkout from the new spec and decode a real diagram. Nothing is committed or
  pushed in tmi-clients.
- `api-schema/tmi-openapi-3.1-experimental.json` (unreferenced copy of all affected schemas)
  is deleted.
- Delivery: commit locally on the feature branch; no push and no PR until Eric says so.

## Schema changes

1. New shared schemas:
   - `CellId`: `type: string, format: uuid` (the current `Cell.id` definition).
   - `CellData`: the current `Cell.properties.data` object, unchanged (`_metadata`,
     `security_boundary`, `data_assets`, `additionalProperties: true`).
2. `Node` becomes a standalone `type: object` with `required: [id, shape]`, properties `id`
   (`$ref CellId`), `shape` (enum `actor, process, store, security-boundary, text-box`,
   pattern `^[a-z][a-z0-9-]*$`, maxLength 64), `data` (`$ref CellData`), plus all of its current
   own properties unchanged (`position, size, angle, attrs, ports, parent, children, x, y,
   width, height`). `additionalProperties` stays unset, as today.
3. `Edge` becomes a standalone `type: object` with `required: [id, shape, source, target]`,
   `id`, `shape` (enum `flow`, same pattern, maxLength 64), `data`, plus its current own
   properties unchanged.
4. `Cell` is deleted. Its `shape` discriminator (no mapping) goes with it.
5. `DfdDiagram.cells.items` and `DfdDiagramInput.cells.items` stay `oneOf [Node, Edge]` with the
   existing `shape` discriminator mapping. These are the only cell discriminators.
6. `DfdDiagram` becomes a single object: all `BaseDiagram` properties plus `cells` and
   `version`; `required` is the union (`id, name, type, created_at, modified_at, cells`).
   `type` is `enum [DFD-1.0.0]`, maxLength 64. `x-discriminator-value` is dropped, and the
   duplicated top-level `version` collapses into one property.
7. `DfdDiagramInput` becomes a single object: all `BaseDiagramInput` properties plus `cells`;
   `required: [name, type, cells]`; `type` keeps minLength 1 and gets maxLength 64.
8. `BaseDiagram`, `BaseDiagramInput` and the deprecated `Diagram` are deleted.
   `ThreatModel.allOf[1].properties.diagrams.items` becomes `$ref DfdDiagram`.
9. Every `shape` and `type` field in the diagram schemas uses maxLength 64, including
   `DiagramListItem.type` (1000 today).
10. Left unchanged: `MinimalCell`/`MinimalNode`/`MinimalEdge`/`MinimalDiagramModel` (already
    flat; the discriminator sits on `MinimalCell`'s own `oneOf`), the AsyncAPI spec's own
    `Cell` (a separate spec), and all examples except where they mention removed schemas.
11. Housekeeping: `scripts/add-tombstone-openapi.jq` targets `DfdDiagram` instead of
    `BaseDiagram`; `vacuum-ruleset.yaml` exemptions that exist only for the removed
    discriminators or `Diagram.oneOf` are removed; the 3.1 experimental file is deleted.

Compatibility: every payload valid today stays valid, because each removed `allOf` base
contributed only properties that the flat schema now declares directly, with the same
constraints except maxLength 1000 → 64 on enum-constrained fields (no enum value is longer
than 64). Request validation (`api/openapi_middleware.go`, kin-openapi) sees the same accepted
set.

## Server code changes (all in `api/`)

- Regenerate `api/api.go` with `make generate-api` (oapi-codegen v2.7.1).
- `Node`, `Edge`, `DfdDiagram`, `DfdDiagram_Cells_Item` and the `Minimal*` types keep their
  names. `Node.Data`/`Edge.Data` change from `*Node_Data`/`*Edge_Data` to the shared
  `*CellData`; adapt callers and `api/node_unmarshal.go` (custom `UnmarshalJSON`/`MarshalJSON`
  on `Node`).
- Replace the `Diagram` wrapper with `DfdDiagram`: `ThreatModel.Diagrams` becomes
  `*[]DfdDiagram`; update `database_store_gorm.go`, `internal_models.go`,
  `threat_model_diagram_handlers.go`, `threat_model_handlers.go`, `websocket.go`,
  `test_fixtures.go` and tests that call `AsDfdDiagram`/`FromDfdDiagram`.
- Delete dead code that exists only to use `Cell`: `CacheCells`/`GetCachedCells` in
  `api/cache_service.go` (no callers) and the unused `DiagramRequest` in `api/types.go`.
- Check the regenerated `FromNode`/`MergeNode`. If they no longer hardcode `shape`, correct
  the comments in `api/cell_union_helpers.go` and the rule in `.claude/CLAUDE.md`. Keep
  `SafeFromNode`/`SafeFromEdge` and `make check-unsafe-union-methods` either way.
- `database_store_gorm.go` changes, so the `oracle-db-admin` reviewer signs off even though
  no database schema changes.

## Tests

Written before the schema change, so they demonstrate the problem or pin today's behavior:

1. Structure test (new, in `api/`): loads the spec and fails if any `allOf` child redeclares
   a property of a base it references, if any discriminator sits on a schema that is not a
   `oneOf`/`anyOf`, or if a discriminator mapping points a schema at a schema that `allOf`s it.
   It fails on today's spec and passes after.
2. Payload corpus test (new, in `api/`): a fixture set of today's diagram payloads (the spec's
   diagram examples, the `test_fixtures.go` diagrams serialized, and a real GET diagram and
   GET threat model response captured from a dev server) validated against the spec with
   kin-openapi, for request (`DfdDiagramInput`) and response (`DfdDiagram`, `ThreatModel`)
   schemas. It passes before and after.
3. Existing gates: `make verify`, `make validate-openapi`, then `make test-integration` and
   the Postman suites against a dev stack (k3s; docker-desktop only after posting on
   general/tmi). CATS remains the branch gate before merge.
4. The Postman data factory (`test/postman/test-data-factory.js`) builds cells with shapes
   `threat-model-process`/`threat-model-datastore`, outside the `Node` enum. Check whether
   those requests already fail today; if so, report it as a separate bug, not part of #956.
5. Clients: regenerate Python, Go and TS in `/Users/efitz/Projects/tmi-clients` from the new
   spec, decode a real diagram from the dev server with cells resolved to `Node`/`Edge`, and
   confirm the patch logs report nothing to fix.

## Out of scope

- Wire-format changes of any kind.
- Removing the tmi-clients safety-net patches.
- AsyncAPI schema changes.
