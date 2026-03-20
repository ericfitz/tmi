# OAuth Stub Lifecycle Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure every test script that needs the OAuth stub automatically starts it if missing and only stops it on exit if it started it — using a single shared helper sourced by all scripts.

**Architecture:** A shared shell library (`scripts/oauth-stub-lib.sh`) provides `ensure_oauth_stub` and `cleanup_oauth_stub`. Each test script sources this library and integrates the two functions into its startup and cleanup. Integration test configs and scripts are also updated to use port 8080 (standard) instead of 8081, and `make start-oauth-stub-test` is removed.

**Tech Stack:** Bash, Make

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `scripts/oauth-stub-lib.sh` | Shared oauth stub lifecycle functions |
| Modify | `test/postman/run-tests.sh` | API test runner — replace custom oauth stub logic with shared helper |
| Modify | `test/postman/run-postman-collection.sh` | Single collection runner — replace custom oauth stub logic with shared helper |
| Modify | `scripts/run-cats-fuzz.sh` | CATS fuzzer — replace `start_oauth_stub()` and cleanup with shared helper |
| Modify | `scripts/run-integration-tests-pg.sh` | PG integration tests — add shared helper, switch port 8081->8080 |
| Modify | `scripts/run-integration-tests-oci.sh` | OCI integration tests — add shared helper, switch port 8081->8080 |
| Modify | `config-test-integration-pg.yml` | PG integration config — change port 8081->8080 |
| Modify | `config-test-integration-oci.yml` | OCI integration config — change port 8081->8080 |
| Modify | `scripts/test-framework.mk` | Remove `start-oauth-stub` Make dependencies from targets; update `test-integration-full` |
| Modify | `Makefile` | Remove `start-oauth-stub-test` target |

---

### Task 1: Create the shared OAuth stub helper library

**Files:**
- Create: `scripts/oauth-stub-lib.sh`

- [ ] **Step 1: Create `scripts/oauth-stub-lib.sh`**

```bash
#!/bin/bash
# oauth-stub-lib.sh - Shared OAuth stub lifecycle management
#
# Source this file from any test script that needs the OAuth stub.
# It provides two functions:
#   ensure_oauth_stub  - Start the stub if not already running
#   cleanup_oauth_stub - Stop the stub only if we started it
#
# Usage:
#   SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#   PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
#   source "${PROJECT_ROOT}/scripts/oauth-stub-lib.sh"
#   ensure_oauth_stub
#   trap cleanup_oauth_stub EXIT
#
# Both functions require PROJECT_ROOT to be set before sourcing.

_OAUTH_STUB_STARTED_BY_US=false

ensure_oauth_stub() {
    if [ -z "${PROJECT_ROOT:-}" ]; then
        echo "[ERROR] PROJECT_ROOT must be set before calling ensure_oauth_stub"
        return 1
    fi

    # Check if stub is already running and responding
    if curl -s http://127.0.0.1:8079/latest >/dev/null 2>&1; then
        echo "[INFO] OAuth stub already running, keeping it"
        _OAUTH_STUB_STARTED_BY_US=false
        return 0
    fi

    # Not running — start it via make target
    echo "[INFO] OAuth stub not running, starting it..."
    if make -C "${PROJECT_ROOT}" start-oauth-stub; then
        # Verify it's responding
        for i in 1 2 3 4 5; do
            if curl -s http://127.0.0.1:8079/latest >/dev/null 2>&1; then
                echo "[INFO] OAuth stub started successfully"
                _OAUTH_STUB_STARTED_BY_US=true
                return 0
            fi
            sleep 1
        done
        echo "[ERROR] OAuth stub started but not responding after 5 seconds"
        return 1
    else
        echo "[ERROR] Failed to start OAuth stub"
        return 1
    fi
}

cleanup_oauth_stub() {
    if [ "${_OAUTH_STUB_STARTED_BY_US}" = "true" ]; then
        echo "[INFO] Stopping OAuth stub (started by this script)..."
        make -C "${PROJECT_ROOT}" stop-oauth-stub 2>/dev/null || true
    fi
}
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x scripts/oauth-stub-lib.sh`

- [ ] **Step 3: Verify it can be sourced without error**

Run: `PROJECT_ROOT=/Users/efitz/Projects/tmi bash -c 'source scripts/oauth-stub-lib.sh && echo OK'`
Expected: `OK`

- [ ] **Step 4: Commit**

```bash
git add scripts/oauth-stub-lib.sh
git commit -m "feat(test): add shared OAuth stub lifecycle helper library"
```

---

### Task 2: Update `test/postman/run-tests.sh`

**Files:**
- Modify: `test/postman/run-tests.sh`

- [ ] **Step 1: Replace the cleanup trap and oauth stub logic**

This script already defines `PROJECT_ROOT` at line 44. Remove these separate blocks (preserving all code between them):

1. Remove the old `cleanup()` function and trap (lines 33-41)
2. Remove the "Check current OAuth stub status" block (lines 58-88)
3. Remove the "Start OAuth stub only if needed" block (lines 90-104)
4. Remove the "Verify stub is running" block (lines 106-115)

Then insert the following right after the `PROJECT_ROOT` assignment (after line 44):

```bash
# Source shared OAuth stub helper
source "${PROJECT_ROOT}/scripts/oauth-stub-lib.sh"

# Setup cleanup trap
cleanup() {
    echo "Cleaning up..."
    cd "$PROJECT_ROOT" 2>/dev/null || true
    cleanup_oauth_stub
}
trap cleanup EXIT INT TERM

# Ensure OAuth stub is running
cd "$PROJECT_ROOT"
ensure_oauth_stub || exit 1
```

Preserve all variable assignments (SCRIPT_DIR, PROJECT_ROOT, OUTPUT_DIR, TIMESTAMP, COLLECTION_FILE, LOG_FILE) and the echo/mkdir statements between the removed blocks.

- [ ] **Step 2: Verify the script still parses**

Run: `bash -n test/postman/run-tests.sh`
Expected: No output (clean parse)

- [ ] **Step 3: Commit**

```bash
git add test/postman/run-tests.sh
git commit -m "refactor(test): use shared oauth stub helper in run-tests.sh"
```

---

### Task 3: Update `test/postman/run-postman-collection.sh`

**Files:**
- Modify: `test/postman/run-postman-collection.sh:29-58` (replace oauth stub management)

- [ ] **Step 1: Replace the cleanup trap and oauth stub logic**

Replace the cleanup function (lines 29-38), and the "Start OAuth stub" + "Verify OAuth stub" block (lines 46-59) with:

```bash
# Source shared OAuth stub helper
source "${PROJECT_ROOT}/scripts/oauth-stub-lib.sh"

# Setup cleanup trap
cleanup() {
    echo "Cleaning up..."
    cd "$PROJECT_ROOT" 2>/dev/null || true
    cleanup_oauth_stub
}
trap cleanup EXIT INT TERM

# Ensure OAuth stub is running
cd "$PROJECT_ROOT"
ensure_oauth_stub || exit 1
echo "OAuth stub is ready"
```

- [ ] **Step 2: Verify the script still parses**

Run: `bash -n test/postman/run-postman-collection.sh`
Expected: No output (clean parse)

- [ ] **Step 3: Commit**

```bash
git add test/postman/run-postman-collection.sh
git commit -m "refactor(test): use shared oauth stub helper in run-postman-collection.sh"
```

---

### Task 4: Update `scripts/run-cats-fuzz.sh`

**Files:**
- Modify: `scripts/run-cats-fuzz.sh:73-79` (cleanup function)
- Modify: `scripts/run-cats-fuzz.sh:147-189` (start_oauth_stub function)
- Modify: `scripts/run-cats-fuzz.sh:458` (call site)

- [ ] **Step 1: Source the shared helper near the top of the file**

After the `PROJECT_ROOT` line (line 21), add:

```bash
source "${PROJECT_ROOT}/scripts/oauth-stub-lib.sh"
```

- [ ] **Step 2: Replace the `cleanup()` function (lines 73-79)**

Replace with:

```bash
cleanup() {
    log "Cleaning up..."
    cleanup_oauth_stub
}
```

- [ ] **Step 3: Remove the `start_oauth_stub()` function entirely (lines 147-189)**

Delete the entire function.

- [ ] **Step 4: Replace the call site (line 458)**

Change `start_oauth_stub` to `ensure_oauth_stub`.

- [ ] **Step 5: Verify the script still parses**

Run: `bash -n scripts/run-cats-fuzz.sh`
Expected: No output (clean parse)

- [ ] **Step 6: Commit**

```bash
git add scripts/run-cats-fuzz.sh
git commit -m "refactor(test): use shared oauth stub helper in run-cats-fuzz.sh"
```

---

### Task 5: Update integration test configs to use port 8080

**Design note:** Previously port 8081 was used to avoid conflicting with a running dev server. By switching to 8080, integration tests will stop any existing dev server (the integration test scripts already call `make stop-server`). This is an accepted trade-off — integration tests can disrupt the dev server.

**Files:**
- Modify: `config-test-integration-pg.yml`
- Modify: `config-test-integration-oci.yml`

- [ ] **Step 1: Update `config-test-integration-pg.yml`**

Change all occurrences of `8081` to `8080`:
- Line 10: `port: "8081"` -> `port: "8080"`
- Line 40: `callback_url: http://localhost:8081/oauth2/callback` -> `http://localhost:8080/oauth2/callback`
- Line 49: `authorization_url: http://localhost:8081/...` -> `http://localhost:8080/...`
- Line 50: `token_url: http://localhost:8081/...` -> `http://localhost:8080/...`
- Line 53: `jwks_url: "http://localhost:8081/..."` -> `"http://localhost:8080/..."`

- [ ] **Step 2: Update `config-test-integration-oci.yml`**

Change all occurrences of `8081` to `8080`:
- Line 17: `port: "8081"` -> `port: "8080"`
- Line 54: `callback_url: http://localhost:8081/oauth2/callback` -> `http://localhost:8080/oauth2/callback`
- Line 63: `authorization_url: http://localhost:8081/...` -> `http://localhost:8080/...`
- Line 64: `token_url: http://localhost:8081/...` -> `http://localhost:8080/...`
- Line 67: `jwks_url: "http://localhost:8081/..."` -> `"http://localhost:8080/..."`

- [ ] **Step 3: Commit**

```bash
git add config-test-integration-pg.yml config-test-integration-oci.yml
git commit -m "fix(test): change integration test server port from 8081 to 8080"
```

---

### Task 6: Update `scripts/run-integration-tests-pg.sh`

**Files:**
- Modify: `scripts/run-integration-tests-pg.sh`

- [ ] **Step 1: Change `SERVER_PORT` from 8081 to 8080 (line 44)**

```bash
SERVER_PORT=8080
```

- [ ] **Step 2: Set PROJECT_ROOT and source the shared helper after `cd` to project root (after line 40)**

The script does `cd "$(dirname "$0")/.."` at line 40 but never assigns `PROJECT_ROOT`. Add both:

```bash
PROJECT_ROOT="$(pwd)"
source "scripts/oauth-stub-lib.sh"
```

- [ ] **Step 3: Replace the cleanup function's oauth stub logic (lines 57-60)**

In the `cleanup()` function, replace:

```bash
    # Always stop OAuth stub (lightweight, doesn't affect next test run)
    if make check-oauth-stub 2>&1 | grep -q "\[SUCCESS\]"; then
        make stop-oauth-stub 2>/dev/null || true
    fi
```

with:

```bash
    cleanup_oauth_stub
```

- [ ] **Step 4: Replace the "Start OAuth stub" block (lines 142-154)**

Replace lines 142-154 (the `make start-oauth-stub-test` call, sleep, and verify block) with:

```bash
# Step 9: Ensure OAuth stub is running for workflow tests
echo "[INFO] Ensuring OAuth stub is running..."
if ensure_oauth_stub; then
    OAUTH_STUB_RUNNING=true
else
    echo "[WARNING] OAuth stub not available - workflow tests will be skipped"
    OAUTH_STUB_RUNNING=false
fi
```

- [ ] **Step 5: Remove the explicit `make stop-oauth-stub` call (lines 208-210)**

Remove these lines — cleanup is now handled by the trap:

```bash
# Stop OAuth stub
echo "[INFO] Stopping OAuth stub..."
make stop-oauth-stub 2>/dev/null || true
```

- [ ] **Step 6: Update the `go test` commands to use `$SERVER_PORT` variable consistently**

Verify all `TEST_SERVER_URL` and `TMI_SERVER_URL` references use `$SERVER_PORT` (they already do via the variable, so the port change propagates automatically).

- [ ] **Step 7: Verify the script still parses**

Run: `bash -n scripts/run-integration-tests-pg.sh`
Expected: No output (clean parse)

- [ ] **Step 8: Commit**

```bash
git add scripts/run-integration-tests-pg.sh
git commit -m "refactor(test): use shared oauth stub helper and port 8080 in PG integration tests"
```

---

### Task 7: Update `scripts/run-integration-tests-oci.sh`

**Files:**
- Modify: `scripts/run-integration-tests-oci.sh`

- [ ] **Step 1: Change `SERVER_PORT` from 8081 to 8080 (line 69)**

```bash
SERVER_PORT=8080
```

- [ ] **Step 2: Set PROJECT_ROOT and source the shared helper after `cd` to project root**

The script does `cd "$(dirname "$0")/.."` at line 65 but never assigns `PROJECT_ROOT`. Add both after the `cd`:

```bash
PROJECT_ROOT="$(pwd)"
source "scripts/oauth-stub-lib.sh"
```

- [ ] **Step 3: Replace the cleanup function's oauth stub logic (lines 107-110)**

In the `cleanup()` function, replace:

```bash
    # Always stop OAuth stub (lightweight, doesn't affect next test run)
    if make check-oauth-stub 2>&1 | grep -q "\[SUCCESS\]"; then
        make stop-oauth-stub 2>/dev/null || true
    fi
```

with:

```bash
    cleanup_oauth_stub
```

- [ ] **Step 4: Add oauth stub startup before running tests**

Before the "Step 6: Run integration tests" section (before line 180), add:

```bash
# Step 6: Ensure OAuth stub is running
echo "[INFO] Ensuring OAuth stub is running..."
ensure_oauth_stub || echo "[WARNING] OAuth stub not available"
```

And renumber "Step 6: Run integration tests" to "Step 7".

- [ ] **Step 5: Verify the script still parses**

Run: `bash -n scripts/run-integration-tests-oci.sh`
Expected: No output (clean parse)

- [ ] **Step 6: Commit**

```bash
git add scripts/run-integration-tests-oci.sh
git commit -m "refactor(test): use shared oauth stub helper and port 8080 in OCI integration tests"
```

---

### Task 8: Update `scripts/test-framework.mk`

**Files:**
- Modify: `scripts/test-framework.mk:13,30,44,57,70,82,99` (remove `start-oauth-stub` dependencies)
- Modify: `scripts/test-framework.mk:110,116` (update `test-integration-full`)

- [ ] **Step 1: Remove `start-oauth-stub` from all target dependencies**

For each of these targets, remove `start-oauth-stub` from the dependency list:
- Line 13: `test-integration-new: start-oauth-stub` -> `test-integration-new:`
- Line 30: `test-integration-tier1: start-oauth-stub` -> `test-integration-tier1:`
- Line 44: `test-integration-tier2: start-oauth-stub` -> `test-integration-tier2:`
- Line 57: `test-integration-tier3: start-oauth-stub` -> `test-integration-tier3:`
- Line 70: `test-integration-all: start-oauth-stub` -> `test-integration-all:`
- Line 82: `test-integration-workflow: start-oauth-stub` -> `test-integration-workflow:`
- Line 99: `test-integration-quick: start-oauth-stub` -> `test-integration-quick:`

The Go test binaries invoked by these targets will need the stub running, but the stub should be managed by the calling script — not as a Make prerequisite (which can't track "did I start it?" state).

- [ ] **Step 2: Update `test-integration-full` (line 116 only)**

On line 116, change `start-oauth-stub-test` to `start-oauth-stub`:
```
$(MAKE) -f $(MAKEFILE_LIST) start-oauth-stub && \
```

Leave the trap on line 110 unchanged — it correctly calls `stop-oauth-stub` because this target owns the full integration lifecycle (it starts everything from scratch).

**Note:** The `test-integration-full` target also starts its own server using `config-test-integration-pg.yml`. After Task 5 updates that config to use port 8080, this target will conflict with any running dev server. This is acceptable — `test-integration-full` calls `stop-server` implicitly via `clean-test-infrastructure`.

- [ ] **Step 3: Commit**

```bash
git add scripts/test-framework.mk
git commit -m "refactor(test): remove oauth stub Make dependencies, use script-managed lifecycle"
```

---

### Task 9: Remove `start-oauth-stub-test` from Makefile

**Files:**
- Modify: `Makefile:939,958-973`

- [ ] **Step 1: Remove `start-oauth-stub-test` from the `.PHONY` declaration (line 939)**

Change:
```makefile
.PHONY: start-oauth-stub start-oauth-stub-test stop-oauth-stub kill-oauth-stub check-oauth-stub
```
to:
```makefile
.PHONY: start-oauth-stub stop-oauth-stub kill-oauth-stub check-oauth-stub
```

- [ ] **Step 2: Remove the `start-oauth-stub-test` target (lines 958-973)**

Delete the entire target block.

- [ ] **Step 3: Verify no remaining references to `start-oauth-stub-test`**

Run: `grep -r "start-oauth-stub-test" . --include='*.sh' --include='*.mk' --include='Makefile'`
Expected: No output

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "chore: remove start-oauth-stub-test Make target"
```

---

### Task 10: End-to-end verification

- [ ] **Step 1: Verify no script references `start-oauth-stub-test`**

Run: `grep -r "start-oauth-stub-test\|oauth-stub-test" . --include='*.sh' --include='*.mk' --include='Makefile' --include='*.yml'`
Expected: No output

- [ ] **Step 2: Verify no script references port 8081 in test contexts**

Run: `grep -rn "8081" config-test-integration-*.yml scripts/run-integration-tests-*.sh`
Expected: No output

- [ ] **Step 3: Verify all modified scripts parse cleanly**

Run:
```bash
bash -n scripts/oauth-stub-lib.sh
bash -n test/postman/run-tests.sh
bash -n test/postman/run-postman-collection.sh
bash -n scripts/run-cats-fuzz.sh
bash -n scripts/run-integration-tests-pg.sh
bash -n scripts/run-integration-tests-oci.sh
```
Expected: No output from any

- [ ] **Step 4: Run lint**

Run: `make lint`
Expected: Pass

- [ ] **Step 5: Run unit tests**

Run: `make test-unit`
Expected: Pass (no functional changes to Go code)

- [ ] **Step 6: Quick smoke test — start oauth stub, run test-api, verify stub stays running**

```bash
make start-oauth-stub
# Verify running
curl -s http://127.0.0.1:8079/latest >/dev/null && echo "RUNNING"
# Run API tests (should NOT stop the stub since it was pre-existing)
make test-api || true
# Verify stub is STILL running after tests
curl -s http://127.0.0.1:8079/latest >/dev/null && echo "STILL RUNNING" || echo "STOPPED - BUG"
```

Expected: Both `RUNNING` and `STILL RUNNING` printed.

- [ ] **Step 7: Quick smoke test — run test-api without stub, verify it starts and stops**

```bash
make stop-oauth-stub
# Verify stopped
curl -s http://127.0.0.1:8079/latest >/dev/null && echo "RUNNING" || echo "STOPPED"
# Run API tests (should start the stub, then stop it on exit)
make test-api || true
# Verify stub was stopped after tests
curl -s http://127.0.0.1:8079/latest >/dev/null && echo "STILL RUNNING - BUG" || echo "STOPPED"
```

Expected: Both `STOPPED` lines printed.

- [ ] **Step 8: Final commit (if any fixups needed)**

```bash
git add -A
git commit -m "fix(test): address verification findings in oauth stub lifecycle"
```
