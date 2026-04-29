# Fix #221: API Schema Too Restrictive for Client-Generated Diagram Data

**Date:** 2026-03-29
**Issue:** [#221](https://github.com/ericfitz/tmi/issues/221)
**Milestone:** 1.4.0

## Problem

The API schema rejects legitimate client-generated diagram data with 400 Bad Request errors. Threat models exported from v1.3.0 cannot round-trip through v1.4.0. Four specific schema restrictions cause failures:

1. **Port group `attrs` rejected** — `PortConfiguration.groups.{name}` has `additionalProperties: false` and only allows `position`, but X6 port groups need `attrs` for circle styling (fill, stroke, radius, magnet behavior).

2. **Port item `attrs` rejected** — Port items have `additionalProperties: false` and only allow `id` and `group`, but X6 port items include `attrs` for text visibility and circle styling.

3. **`refX`/`refY` type mismatch** — Schema declares `type: number` but client sends percentage strings like `"50%"`. Both `0.5` and `"50%"` are valid X6 positioning values.

4. **`rgb()` color pattern rejects spaces** — Pattern `rgb\([0-9]*,[0-9]*,[0-9]*\)` doesn't allow spaces after commas, but CSS and the client produce `rgb(230, 240, 255)`.

## Root Cause

- Issues 1-2: Commit `c0757ea6` (2025-11-18) bulk-added `additionalProperties: false` to 107 schemas for injection protection without verifying what the client actually sends.
- Issue 3: Commit `49946733` (2026-01-18) introduced `refX`/`refY` as number-only from the start.
- Issue 4: Color pattern authored without considering CSS whitespace tolerance in `rgb()`.

## Design

### Approach: Selective relaxation (Approach C)

Keep `additionalProperties: false` on `PortConfiguration` itself (structural integrity) but remove it from the inner port group and port item objects, which contain X6 rendering metadata the server stores opaquely.

### Changes to `api-schema/tmi-openapi.json`

#### 1. Port group objects

Remove `additionalProperties: false` from the port group additionalProperties schema. Add `attrs` property as `type: object` with no further constraints.

**Before:**
```json
"additionalProperties": {
  "type": "object",
  "properties": {
    "position": { ... }
  },
  "additionalProperties": false
}
```

**After:**
```json
"additionalProperties": {
  "type": "object",
  "properties": {
    "position": { ... },
    "attrs": {
      "type": "object",
      "description": "Visual attributes for port group rendering (e.g., circle styling)"
    }
  }
}
```

#### 2. Port item objects

Remove `additionalProperties: false` from port items. Add `attrs` property as `type: object`.

**Before:**
```json
"properties": {
  "id": { ... },
  "group": { ... }
},
"additionalProperties": false
```

**After:**
```json
"properties": {
  "id": { ... },
  "group": { ... },
  "attrs": {
    "type": "object",
    "description": "Visual attributes for port rendering (e.g., text visibility, circle styling)"
  }
}
```

#### 3. `refX` and `refY` type

Change from `type: number` to `oneOf` accepting both number and percentage string.

**Before:**
```json
"refX": {
  "type": "number",
  "description": "Horizontal position (0-1 relative or pixels)"
}
```

**After:**
```json
"refX": {
  "oneOf": [
    { "type": "number" },
    { "type": "string", "pattern": "^\\d+%$" }
  ],
  "description": "Horizontal position (0-1 relative, pixels, or percentage string e.g. '50%')"
}
```

Same change for `refY`.

#### 4. Color pattern

Update `rgb()` pattern on `NodeAttrs.body.fill`, `NodeAttrs.body.stroke`, and `NodeAttrs.text.fill` to allow optional spaces.

**Before:**
```
rgb\([0-9]*,[0-9]*,[0-9]*\)
```

**After:**
```
rgb\( *[0-9]+ *, *[0-9]+ *, *[0-9]+ *\)
```

#### 5. Update examples

Update `PortConfiguration` example to include `attrs` on both groups and items.

### What we're NOT changing

- `CreateDiagramRequest` — intentionally minimal, client already handles this
- `additionalProperties: false` on `PortConfiguration` top-level
- `additionalProperties: false` on `NodeAttrs`, `NodeAttrs.body`, `NodeAttrs.text`, `EdgeAttrs`
- No Go handler changes needed — purely schema validation

### Verification

1. `make validate-openapi`
2. `make generate-api`
3. `make build-server`
4. `make test-unit`
