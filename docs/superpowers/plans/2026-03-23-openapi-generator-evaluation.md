# openapi-generator Evaluation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evaluate openapi-generator as a replacement for oapi-codegen to enable OpenAPI 3.1 migration, producing a go/no-go decision with empirical evidence.

**Architecture:** Four sequential phases — spec conversion, code generation, middleware prototype, evaluation report. Each phase builds on the previous and produces a standalone artifact. The spec conversion has value regardless of the generator decision.

**Tech Stack:** Go, openapi-generator-cli (Homebrew), Vacuum (validation), jq (spec transformation), Gin web framework

**Spec:** `docs/superpowers/specs/2026-03-23-openapi-generator-evaluation-design.md`

---

## Task 1: Install openapi-generator

**Files:**
- None (tool installation only)

- [ ] **Step 1: Install openapi-generator via Homebrew**

Note: The Homebrew formula includes a JVM dependency (OpenJDK). This will be installed automatically if not already present.

```bash
brew install openapi-generator
```

- [ ] **Step 2: Verify installation**

```bash
openapi-generator version
java -version
```

Expected: openapi-generator version 7.x (e.g., `7.12.0`), Java version present.

- [ ] **Step 3: Verify Vacuum is available**

```bash
vacuum version
```

Expected: Version output confirming 3.1 support

---

## Task 2: Convert OpenAPI spec from 3.0.3 to 3.1

**Files:**
- Read: `api-schema/tmi-openapi.json` (1.9 MB, use jq streaming)
- Create: `api-schema/tmi-openapi-3.1.json`

The spec is ~1.9 MB. Use jq for all transformations. The conversion must handle:
- 145 `nullable: true` instances → type arrays with `"null"`
- 0 `exclusiveMinimum`/`exclusiveMaximum` boolean instances (none to convert)
- Version bump from `3.0.3` to `3.1.0`
- Add `jsonSchemaDialect`
- Keep `example` as-is (deprecated in 3.1 but still valid)

- [ ] **Step 1: Create backup and verify source spec**

```bash
cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.backup
jq '.openapi' api-schema/tmi-openapi.json

# Pre-conversion check: ensure no nullable fields already have array types
jq '[.. | objects | select(.nullable == true and (.type | type) == "array")] | length' api-schema/tmi-openapi.json
```

Expected: version `"3.0.3"`, array-typed nullable count `0`

- [ ] **Step 2: Write the jq conversion script**

Create `scripts/convert-openapi-3.0-to-3.1.sh`:

```bash
#!/usr/bin/env bash
# Convert OpenAPI 3.0.3 spec to 3.1.0
# Usage: ./scripts/convert-openapi-3.0-to-3.1.sh api-schema/tmi-openapi.json api-schema/tmi-openapi-3.1.json

set -euo pipefail

INPUT="${1:?Usage: $0 <input.json> <output.json>}"
OUTPUT="${2:?Usage: $0 <input.json> <output.json>}"

if [ ! -f "$INPUT" ]; then
  echo "Error: Input file not found: $INPUT" >&2
  exit 1
fi

echo "Converting $INPUT -> $OUTPUT"
echo "  OpenAPI 3.0.3 -> 3.1.0"

jq '
  # 1. Bump version
  .openapi = "3.1.0" |

  # 2. Add JSON Schema dialect
  .jsonSchemaDialect = "https://json-schema.org/draft/2020-12/schema" |

  # 3. Convert nullable: true to type arrays
  # Walk all objects: if nullable==true and type is a string, convert to array with null
  (.. | objects | select(.nullable == true and (.type | type) == "string")) |=
    ((.type = [.type, "null"]) | del(.nullable)) |

  # 4. Convert nullable: true without explicit type (just remove nullable flag)
  (.. | objects | select(.nullable == true and .type == null)) |=
    del(.nullable) |

  # 5. Convert exclusiveMinimum/exclusiveMaximum boolean style to numeric style
  # (TMI has 0 instances, but included for correctness)
  (.. | objects | select(.exclusiveMinimum == true)) |=
    ((.exclusiveMinimum = .minimum) | del(.minimum)) |
  (.. | objects | select(.exclusiveMaximum == true)) |=
    ((.exclusiveMaximum = .maximum) | del(.maximum))
' "$INPUT" > "$OUTPUT"

# Verify output is valid JSON
if jq empty "$OUTPUT" 2>/dev/null; then
  echo "  Output is valid JSON"
else
  echo "Error: Output is not valid JSON" >&2
  exit 1
fi

# Report conversion stats
NULLABLE_REMAINING=$(jq '[.. | objects | select(.nullable != null)] | length' "$OUTPUT")
VERSION=$(jq -r '.openapi' "$OUTPUT")
echo "  Output version: $VERSION"
echo "  Remaining nullable fields: $NULLABLE_REMAINING (should be 0)"

echo "Done."
```

- [ ] **Step 3: Run the conversion**

```bash
chmod +x scripts/convert-openapi-3.0-to-3.1.sh
./scripts/convert-openapi-3.0-to-3.1.sh api-schema/tmi-openapi.json api-schema/tmi-openapi-3.1.json
```

Expected:
- Output version: `3.1.0`
- Remaining nullable fields: `0`
- Valid JSON output

- [ ] **Step 4: Verify key properties were preserved**

```bash
# Verify path count is unchanged
echo "3.0.3 paths: $(jq '.paths | length' api-schema/tmi-openapi.json)"
echo "3.1.0 paths: $(jq '.paths | length' api-schema/tmi-openapi-3.1.json)"

# Verify schema count is unchanged
echo "3.0.3 schemas: $(jq '.components.schemas | length' api-schema/tmi-openapi.json)"
echo "3.1.0 schemas: $(jq '.components.schemas | length' api-schema/tmi-openapi-3.1.json)"

# Verify discriminators preserved
echo "3.0.3 discriminators: $(jq '[.. | objects | select(.discriminator != null)] | length' api-schema/tmi-openapi.json)"
echo "3.1.0 discriminators: $(jq '[.. | objects | select(.discriminator != null)] | length' api-schema/tmi-openapi-3.1.json)"

# Verify vendor extensions preserved
echo "3.0.3 x-public-endpoint: $(jq '[.. | objects | select(."x-public-endpoint" != null)] | length' api-schema/tmi-openapi.json)"
echo "3.1.0 x-public-endpoint: $(jq '[.. | objects | select(."x-public-endpoint" != null)] | length' api-schema/tmi-openapi-3.1.json)"

# Spot-check a nullable conversion
echo "Sample nullable conversion:"
jq '.components.schemas.ThreatModelInput.properties.description.type' api-schema/tmi-openapi-3.1.json
```

Expected: All counts match between versions. The sample nullable should show `["string", "null"]` instead of `"string"`.

- [ ] **Step 5: Validate with Vacuum**

```bash
# Use project's Vacuum ruleset (at project root, same as Makefile's validate-openapi target)
vacuum lint api-schema/tmi-openapi-3.1.json --details -r vacuum-ruleset.yaml 2>&1 | tee /tmp/vacuum-3.1-output.txt
echo "Exit code: $?"
```

Review output. Acceptance: zero errors. Warnings should be reviewed and documented. If Vacuum reports errors related to the 3.1 conversion (not pre-existing), fix and re-run.

Note: If Vacuum's ruleset does not support 3.1, try without the custom ruleset:

```bash
vacuum lint api-schema/tmi-openapi-3.1.json --details 2>&1 | tee /tmp/vacuum-3.1-default.txt
```

- [ ] **Step 6: Clean up and commit**

```bash
rm api-schema/tmi-openapi.json.backup
```

```bash
git add api-schema/tmi-openapi-3.1.json scripts/convert-openapi-3.0-to-3.1.sh
git commit -m "feat(api): add OpenAPI 3.1 version of specification (#180)

Convert tmi-openapi.json (3.0.3) to tmi-openapi-3.1.json (3.1.0).
Transformations: nullable->type arrays, version bump, jsonSchemaDialect.
Original 3.0.3 spec preserved unchanged.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Generate code with openapi-generator (go-gin-server)

**Files:**
- Read: `api-schema/tmi-openapi-3.1.json`
- Create: `eval/openapi-generator-output/` (entire directory)

- [ ] **Step 1: Create eval directory and run generator**

```bash
mkdir -p eval/openapi-generator-output
openapi-generator generate \
  -i api-schema/tmi-openapi-3.1.json \
  -g go-gin-server \
  -o eval/openapi-generator-output \
  --additional-properties=packageName=api \
  2>&1 | tee eval/openapi-generator-output/generation-log.txt
echo "Exit code: $?"
```

Review the generation log for warnings and errors. Document any skipped operations or unsupported features.

- [ ] **Step 2: Inventory generated files**

```bash
find eval/openapi-generator-output -name "*.go" | sort
wc -l eval/openapi-generator-output/**/*.go 2>/dev/null || find eval/openapi-generator-output -name "*.go" -exec wc -l {} +
```

Document: how many files, total lines, directory structure.

- [ ] **Step 3: Attempt compilation**

```bash
cd eval/openapi-generator-output
go mod init github.com/ericfitz/tmi/eval/openapi-generator-output 2>/dev/null
go mod tidy 2>&1 | tee /tmp/go-mod-tidy-output.txt
go build ./... 2>&1 | tee /tmp/go-build-output.txt
echo "Exit code: $?"
cd -
```

Document: does it compile? If not, what are the errors? Categorize errors as:
- Generator bugs (report upstream)
- TMI-specific spec issues (fixable)
- Fundamental limitations (potential blocker)

---

## Task 4: Analyze generated models

**Files:**
- Read: generated model files in `eval/openapi-generator-output/`
- Read: `api/api.go` (current oapi-codegen output, for comparison)

- [ ] **Step 1: Check allOf composition**

Find a schema that uses allOf in the spec (e.g., `ThreatModelInput` or one of the cell types) and examine the generated Go struct.

```bash
# Find allOf schemas in the spec
jq -r '.components.schemas | to_entries[] | select(.value.allOf != null) | .key' api-schema/tmi-openapi-3.1.json | head -10

# Look at the generated struct for one of these
grep -A 30 "type ThreatModel " eval/openapi-generator-output/**/*.go 2>/dev/null || \
  find eval/openapi-generator-output -name "*.go" -exec grep -l "ThreatModel" {} \;
```

Evaluate: Are fields from all allOf members merged correctly into one struct? Are there missing fields?

- [ ] **Step 2: Check discriminated unions**

```bash
# Find discriminator schemas
jq -r '.components.schemas | to_entries[] | select(.value.discriminator != null) | .key' api-schema/tmi-openapi-3.1.json

# Examine generated code for these types
grep -rn "Node\|Edge\|DfdDiagram" eval/openapi-generator-output/ --include="*.go" | head -30
```

Evaluate: How does openapi-generator handle the multi-value discriminator mapping (multiple node shapes → same Go type)? Is it better, worse, or equivalent to oapi-codegen's broken behavior (which requires `cell_union_helpers.go` workaround)?

- [ ] **Step 3: Check nullable type representation**

```bash
# Find a nullable field in generated code
grep -rn "null\|Nullable\|Optional\|omitempty" eval/openapi-generator-output/ --include="*.go" | head -20
```

Evaluate: Are nullable fields represented as pointers (`*string`), wrapper types, or something else? Is the representation idiomatic Go?

- [ ] **Step 4: Check JSON Patch handling**

```bash
# Check if JSON Patch content type is in generated code
grep -rn "json-patch\|JsonPatch\|PatchDocument" eval/openapi-generator-output/ --include="*.go" | head -10

# Check generation log for skipped operations
grep -i "skip\|unsupported\|json-patch" eval/openapi-generator-output/generation-log.txt
```

Evaluate: Were PATCH endpoints with `application/json-patch+json` content type generated or silently dropped? If dropped, can they be added manually?

- [ ] **Step 5: Document model analysis findings**

Create `eval/model-analysis.md` with findings for each criterion:
- allOf: pass/fail + details
- Discriminators: pass/fail + details
- Nullable: pass/fail + details
- JSON Patch: pass/fail + details
- Overall code quality assessment

---

## Task 5: Analyze generated server stubs

**Files:**
- Read: generated router/handler files in `eval/openapi-generator-output/`
- Read: `api/api.go:1-50` (current ServerInterface for comparison)

- [ ] **Step 1: Examine handler function signatures**

```bash
# Find handler function definitions
grep -rn "func.*Handler\|func.*Controller\|type.*Interface" eval/openapi-generator-output/ --include="*.go" | head -30

# Check if handlers use gin.Context
grep -rn "gin.Context\|gin.HandlerFunc" eval/openapi-generator-output/ --include="*.go" | head -20
```

Evaluate: Do handlers take `*gin.Context`? Is there a `ServerInterface`-like pattern, or a different routing approach? How different is the handler signature from oapi-codegen's `Method(c *gin.Context)` pattern?

- [ ] **Step 2: Examine router setup**

```bash
# Find router setup code
grep -rn "router\|Router\|gin.Default\|gin.New\|r.GET\|r.POST" eval/openapi-generator-output/ --include="*.go" | head -30
```

Evaluate: How are routes registered? Is there a central router that Gin middleware can be applied to? Can middleware be inserted into the chain?

- [ ] **Step 3: Check path parameter handling**

```bash
# Look for path parameter extraction
grep -rn "threat_model_id\|asset_id\|Param\|param" eval/openapi-generator-output/ --include="*.go" | head -20
```

Evaluate: Are path parameters extracted correctly? Do they match the spec's naming?

- [ ] **Step 4: Document server stub findings**

Append to `eval/model-analysis.md`:
- Handler signatures: compatible with Gin middleware? How much adaptation needed?
- Router pattern: can middleware be inserted?
- Path parameters: correct?
- Content type handling: multiple content types per endpoint?

---

## Task 6: Test unevaluatedProperties enforcement

**Files:**
- Modify: `api-schema/tmi-openapi-3.1.json` (temporary modification for testing)
- Read: generated output

- [ ] **Step 1: Add unevaluatedProperties to test schemas**

Pick 2-3 schemas that use allOf composition. Add `"unevaluatedProperties": false` to them in the 3.1 spec.

```bash
# Find candidate schemas (allOf + properties)
jq -r '.components.schemas | to_entries[] | select(.value.allOf != null) | .key' api-schema/tmi-openapi-3.1.json | head -5
```

Use jq to add `unevaluatedProperties: false` to the chosen schemas. For example:

```bash
# Add unevaluatedProperties to specific schemas (replace SCHEMA_NAME with actual names)
jq '.components.schemas.SCHEMA_NAME.unevaluatedProperties = false' api-schema/tmi-openapi-3.1.json > /tmp/tmi-openapi-3.1-uneval.json
```

- [ ] **Step 2: Regenerate with unevaluatedProperties**

```bash
mkdir -p eval/openapi-generator-uneval
openapi-generator generate \
  -i /tmp/tmi-openapi-3.1-uneval.json \
  -g go-gin-server \
  -o eval/openapi-generator-uneval \
  --additional-properties=packageName=api \
  2>&1 | tee eval/openapi-generator-uneval/generation-log.txt
```

- [ ] **Step 3: Compare output**

```bash
diff -r eval/openapi-generator-output eval/openapi-generator-uneval --exclude=generation-log.txt | head -50
```

Evaluate: Did `unevaluatedProperties: false` change the generated code? Does the generated code include validation that rejects unknown properties? If the output is identical, the generator parses but ignores the constraint — this is a potential No-Go.

- [ ] **Step 4: Clean up temporary files**

```bash
rm -rf eval/openapi-generator-uneval /tmp/tmi-openapi-3.1-uneval.json
```

- [ ] **Step 5: Document unevaluatedProperties findings**

Append to `eval/model-analysis.md`:
- Does openapi-generator recognize `unevaluatedProperties`?
- Does it generate enforcement code?
- If not: is this a blocker? (It is the primary reason for the 3.1 migration.)

---

## Task 7: Prototype JWT middleware integration

**Files:**
- Create: `eval/middleware-prototype/main.go`
- Create: `eval/middleware-prototype/go.mod`
- Read: `cmd/server/main.go` (JWTMiddleware function, currently ~line 208)
- Read: generated router setup from `eval/openapi-generator-output/`

This task depends on the findings from Tasks 3-5. If the generated code does not compile or does not use Gin, adjust the prototype approach accordingly.

- [ ] **Step 1: Identify the generated router entry point**

Based on Task 5 findings, identify how the generated code sets up its Gin router and how handlers are registered. Document the entry point file and function.

- [ ] **Step 2: Create the middleware prototype**

Create `eval/middleware-prototype/main.go` that:
1. Creates a Gin engine
2. Applies TMI's `JWTMiddleware` (or a simplified version that just reads/writes `gin.Context`)
3. Registers one generated endpoint handler
4. Demonstrates the middleware executes before the handler

The exact code depends on the generated handler signatures discovered in Task 5. The prototype should be minimal — just enough to prove the integration pattern works.

```go
// eval/middleware-prototype/main.go
// Skeleton — adapt based on Task 5 findings
package main

import (
    "fmt"
    "net/http"

    "github.com/gin-gonic/gin"
)

// SimulatedJWTMiddleware mimics TMI's JWT middleware pattern:
// reads Authorization header, sets user context on gin.Context
func SimulatedJWTMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        auth := c.GetHeader("Authorization")
        if auth == "" {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing auth"})
            return
        }
        // Simulate setting user context (as TMI's real JWT middleware does)
        c.Set("user_id", "test-user")
        c.Set("user_email", "test@example.com")
        c.Next()
    }
}

func main() {
    r := gin.Default()

    // Apply middleware globally (as TMI does)
    r.Use(SimulatedJWTMiddleware())

    // TODO: Register one generated handler here
    // The exact integration depends on Task 5 findings:
    // - If generated code provides gin.HandlerFunc: register directly
    // - If generated code uses a different pattern: write an adapter

    r.GET("/threat_models", func(c *gin.Context) {
        userID, _ := c.Get("user_id")
        c.JSON(200, gin.H{"message": "handler reached", "user": userID})
    })

    fmt.Println("Prototype listening on :8099")
    r.Run(":8099")
}
```

- [ ] **Step 3: Verify compilation**

```bash
cd eval/middleware-prototype
go mod init github.com/ericfitz/tmi/eval/middleware-prototype
go mod tidy
go build ./...
echo "Exit code: $?"
cd -
```

- [ ] **Step 4: Test the middleware chain (manual)**

Note: This is isolated evaluation code, not part of the main TMI build system, so direct `go build`/`go run` is acceptable here (no Makefile target exists for eval code).

```bash
# Build and run the prototype
cd eval/middleware-prototype && go build -o prototype . && ./prototype &
sleep 2

# Test without auth (should get 401)
curl -s -w "\n%{http_code}\n" http://localhost:8099/threat_models
# Expected: 401

# Test with auth (should get 200)
curl -s -w "\n%{http_code}\n" -H "Authorization: Bearer test" http://localhost:8099/threat_models
# Expected: 200 with user context

# Clean up
kill %1 2>/dev/null
cd -
```

- [ ] **Step 5: Document middleware prototype findings**

Append to `eval/model-analysis.md`:
- Does the Gin middleware chain work with generated handlers?
- What adapter code (if any) is needed?
- Estimated effort to adapt all 15+ middleware files

---

## Task 8: Assess runtime request validation options

**Files:**
- Read: `api/openapi_middleware.go` (current kin-openapi dependency)
- Research: pb33f/libopenapi-validator, openapi-generator validation features

This is a research task, not a coding task. The goal is to document viable paths for runtime request validation after migrating away from oapi-codegen.

- [ ] **Step 1: Check if openapi-generator generates validation code**

```bash
# Look for validation in generated code
grep -rn "validate\|Validate\|Required\|required\|binding:" eval/openapi-generator-output/ --include="*.go" | head -20
```

- [ ] **Step 2: Research pb33f/libopenapi-validator**

Check if pb33f/libopenapi-validator supports OpenAPI 3.1 and can replace kin-openapi for runtime validation. Key questions:
- Does it support 3.1?
- Does it have a Gin middleware adapter?
- Is it actively maintained?
- Does it support `unevaluatedProperties`?

```bash
# Check the library exists and its latest version
go list -m -versions github.com/pb33f/libopenapi-validator 2>/dev/null || echo "not in go module cache"
```

- [ ] **Step 3: Document validation options**

Append to `eval/model-analysis.md` a section on runtime validation:
- Option A: openapi-generator provides validation → describe what's generated
- Option B: pb33f/libopenapi-validator → feasibility assessment
- Option C: Keep kin-openapi for validation only → partial migration cost
- Option D: Rely on generated struct validation (binding tags) → coverage gaps
- Recommendation for which option to pursue

---

## Task 9: Fallback — test go-server generator (if needed)

**Files:**
- Create: `eval/openapi-generator-go-server/` (if go-gin-server is unusable)

**This task is conditional.** Only execute if `go-gin-server` output is fundamentally unusable (doesn't compile, doesn't use Gin, or critical features are missing).

- [ ] **Step 1: Generate with go-server**

```bash
mkdir -p eval/openapi-generator-go-server
openapi-generator generate \
  -i api-schema/tmi-openapi-3.1.json \
  -g go-server \
  -o eval/openapi-generator-go-server \
  --additional-properties=packageName=api \
  2>&1 | tee eval/openapi-generator-go-server/generation-log.txt
```

- [ ] **Step 2: Quick assessment**

Check if `go-server` output is stdlib `net/http` based and can wrap with Gin:

```bash
grep -rn "http.Handler\|http.HandlerFunc\|ServeHTTP\|mux" eval/openapi-generator-go-server/ --include="*.go" | head -20
```

- [ ] **Step 3: Document fallback findings**

Append to `eval/model-analysis.md`:
- Is `go-server` a viable alternative?
- Can its `http.Handler` be wrapped with `gin.WrapH()`?
- What's the estimated adaptation effort vs `go-gin-server`?

---

## Task 10: Write go/no-go evaluation report

**Files:**
- Read: `eval/model-analysis.md` (all findings from tasks 4-9)
- Modify: `docs/superpowers/specs/2026-03-23-openapi-generator-evaluation-design.md` (append report)

- [ ] **Step 1: Compile decision matrix**

Based on all findings, fill in the decision criteria table from the design spec:

| Criteria | Result | Evidence |
|---|---|---|
| allOf composition | ? | Task 4 Step 1 findings |
| Discriminated unions | ? | Task 4 Step 2 findings |
| JSON Patch endpoints | ? | Task 4 Step 4 findings |
| Gin middleware compat | ? | Task 7 findings |
| Runtime request validation | ? | Task 8 findings |
| Nullable types | ? | Task 4 Step 3 findings |
| Code quality | ? | Overall assessment |
| `unevaluatedProperties` | ? | Task 6 findings |

- [ ] **Step 2: Write the evaluation report**

Append an `## Evaluation Report` section to the design spec with:
- Date of evaluation
- openapi-generator version tested
- Summary of each criterion (pass/fail/caveat)
- Blocking issues (if any)
- Workarounds needed (if any)
- **Decision: Go / Go with caveats / No-go**
- Recommended next steps

- [ ] **Step 3: Add .gitignore for eval build artifacts**

```bash
cat > eval/.gitignore << 'EOF'
# Build artifacts from evaluation code
*.exe
vendor/
prototype
EOF
```

- [ ] **Step 4: Commit all evaluation artifacts**

```bash
git add eval/ api-schema/tmi-openapi-3.1.json docs/superpowers/specs/2026-03-23-openapi-generator-evaluation-design.md
git commit -m "docs: complete openapi-generator evaluation (#180)

Evaluate openapi-generator go-gin-server as oapi-codegen replacement.
Includes: OpenAPI 3.1 spec conversion, generated code analysis,
middleware integration prototype, and go/no-go decision.

Decision: [GO/GO_WITH_CAVEATS/NO_GO]

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 5: Push to remote**

```bash
git pull --rebase
git push
git status  # MUST show "up to date with origin"
```

- [ ] **Step 6: Update GitHub issue #180**

Post a comment on issue #180 summarizing the evaluation results and decision. If the decision is Go, outline next steps for the migration plan. If No-Go, recommend the fallback path.

```bash
gh issue comment 180 --repo ericfitz/tmi --body "## Evaluation Complete

**Decision:** [Go/Go with caveats/No-go]

[Summary of key findings]

**Next steps:** [What happens next based on the decision]

See full report: docs/superpowers/specs/2026-03-23-openapi-generator-evaluation-design.md"
```
