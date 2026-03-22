# Makefile Refactoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clean up the TMI Makefile by removing dead code, fixing bugs, consolidating duplicate targets, standardizing naming, and extracting reusable macros.

**Architecture:** Pure Makefile refactoring — no Go code, no shell scripts, no functionality changes. Two files: `Makefile` and `scripts/test-framework.mk`. Changes are ordered to minimize risk: remove dead code first (safe), fix bugs (safe), consolidate targets (medium), extract macros (requires care).

**Tech Stack:** GNU Make, shell

**Spec:** `docs/superpowers/specs/2026-03-21-makefile-refactoring-design.md`

---

### Task 1: Add reusable macros near top of Makefile

Add the four macro definitions after the existing logging macros (after line 49). These must exist before any target uses them.

**Files:**
- Modify: `Makefile:49` (insert after `log_error` definition)

- [ ] **Step 1: Add macro definitions**

Insert after line 49 (after the `log_error` endef):

```makefile

# ============================================================================
# REUSABLE MACROS
# ============================================================================

# Graceful process kill: SIGTERM, wait, SIGKILL if still alive
# Usage: @$(call graceful_kill,PID_VALUE)
define graceful_kill
PID=$(1); \
if [ -n "$$PID" ] && ps -p $$PID > /dev/null 2>&1; then \
	kill $$PID 2>/dev/null || true; \
	sleep 1; \
	if ps -p $$PID > /dev/null 2>&1; then \
		kill -9 $$PID 2>/dev/null || true; \
	fi; \
fi
endef

# Kill all processes on a port: SIGTERM all, wait, SIGKILL survivors
# Usage: @$(call kill_port,PORT_NUMBER)
define kill_port
PIDS=$$(lsof -ti :$(1) 2>/dev/null || true); \
if [ -n "$$PIDS" ]; then \
	for PID in $$PIDS; do \
		kill $$PID 2>/dev/null || true; \
	done; \
	sleep 1; \
	PIDS=$$(lsof -ti :$(1) 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		for PID in $$PIDS; do \
			kill -9 $$PID 2>/dev/null || true; \
		done; \
	fi; \
fi
endef

# Idempotent Docker container start: create if missing, start if stopped, no-op if running
# Usage: @$(call ensure_container,NAME,HOST_PORT,CONTAINER_PORT,IMAGE,EXTRA_DOCKER_ARGS)
define ensure_container
if ! docker ps -a --format "{{.Names}}" | grep -q "^$(1)$$"; then \
	echo -e "$(BLUE)[INFO]$(NC) Creating container $(1)..."; \
	docker run -d --name $(1) -p 127.0.0.1:$(2):$(3) $(5) $(4); \
elif ! docker ps --format "{{.Names}}" | grep -q "^$(1)$$"; then \
	echo -e "$(BLUE)[INFO]$(NC) Starting container $(1)..."; \
	docker start $(1); \
fi; \
echo "✅ $(1) running on port $(2)"
endef

# Poll until a health check command succeeds
# Usage: @$(call wait_for_ready,HEALTH_CHECK_CMD,TIMEOUT_SECONDS,SERVICE_NAME)
define wait_for_ready
timeout=$(2); \
while [ $$timeout -gt 0 ]; do \
	if $(1) >/dev/null 2>&1; then \
		echo -e "$(GREEN)[SUCCESS]$(NC) $(3) is ready!"; \
		break; \
	fi; \
	sleep 2; \
	timeout=$$((timeout - 2)); \
done; \
if [ $$timeout -le 0 ]; then \
	echo -e "$(RED)[ERROR]$(NC) $(3) failed to start within $(2) seconds"; \
	exit 1; \
fi
endef
```

- [ ] **Step 2: Verify Makefile still parses**

Run: `make -n help >/dev/null 2>&1 && echo "OK"`
Expected: `OK`

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "refactor(make): add reusable macros for process, port, container, and health check management"
```

---

### Task 2: Refactor infrastructure targets to use macros

Replace the duplicated container start/stop/wait logic with macro calls.

**Files:**
- Modify: `Makefile` — `start-database`, `start-redis`, `start-test-database`, `start-test-redis`, `stop-process`, `wait-database`, `wait-test-database`, `wait-process`

- [ ] **Step 1: Refactor `start-database` (line ~80)**

Replace the body with:

```makefile
start-database:
	$(call log_info,Starting PostgreSQL container...)
	@CONTAINER="$(INFRASTRUCTURE_POSTGRES_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-postgresql"; fi; \
	PORT="$(INFRASTRUCTURE_POSTGRES_PORT)"; \
	if [ -z "$$PORT" ]; then PORT="5432"; fi; \
	USER="$(INFRASTRUCTURE_POSTGRES_USER)"; \
	if [ -z "$$USER" ]; then USER="tmi_dev"; fi; \
	PASSWORD="$(INFRASTRUCTURE_POSTGRES_PASSWORD)"; \
	if [ -z "$$PASSWORD" ]; then PASSWORD="dev123"; fi; \
	DATABASE="$(INFRASTRUCTURE_POSTGRES_DATABASE)"; \
	if [ -z "$$DATABASE" ]; then DATABASE="tmi_dev"; fi; \
	IMAGE="$(INFRASTRUCTURE_POSTGRES_IMAGE)"; \
	if [ -z "$$IMAGE" ]; then IMAGE="tmi/tmi-postgresql:latest"; fi; \
	DATA_DIR="$$HOME/Projects/tmi-postgres-data"; \
	mkdir -p "$$DATA_DIR"; \
	$(call ensure_container,$$CONTAINER,$$PORT,5432,$$IMAGE,-e POSTGRES_USER=$$USER -e POSTGRES_PASSWORD=$$PASSWORD -e POSTGRES_DB=$$DATABASE -v "$$DATA_DIR:/var/lib/postgresql/data")
```

Note: The `ensure_container` macro is expanded at Make parse time, but its `$(1)` etc. parameters will contain shell variable references (`$$CONTAINER`) which are evaluated at runtime. This means we can't use the macro directly with shell variables as parameters — the macro uses Make's `$(1)` substitution which happens before the shell sees the command. We need a different approach.

**Revised approach:** Since the container targets have complex per-target config variable resolution that must happen in shell, the `ensure_container` macro is best applied to the simpler targets (test containers, Redis) where values are hardcoded. For `start-database` and `start-promtail` which read config variables, keep the existing pattern but consolidate the shared docker create-or-start logic.

Actually, since all the config variable resolution happens in shell (`$$CONTAINER`, `$$PORT`, etc.), and GNU Make's `$(call)` does text substitution before the shell runs, we cannot pass shell variables to `$(call ensure_container,...)`. The macro only works with literal values known at Make parse time.

**Revised Step 1: Refactor test containers and simple containers with `ensure_container`**

These have hardcoded values, so the macro works directly:

Replace `start-test-database` (line ~164) with:

```makefile
start-test-database:
	$(call log_info,Starting test PostgreSQL container - ephemeral...)
	@$(call ensure_container,tmi-postgresql-test,5433,5432,tmi/tmi-postgresql:latest,-e POSTGRES_USER=tmi_dev -e POSTGRES_PASSWORD=dev123 -e POSTGRES_DB=tmi_dev)
```

Replace `start-test-redis` (line ~197) with:

```makefile
start-test-redis:
	$(call log_info,Starting test Redis container...)
	@$(call ensure_container,tmi-redis-test,6380,6379,tmi/tmi-redis:latest,)
```

Leave `start-database`, `start-redis`, and `start-promtail` as-is — they use shell-resolved config variables that can't be passed to Make macros. (Spec Section 7c lists `start-promtail` as a candidate, but it has the same shell-variable limitation as `start-database`.)

- [ ] **Step 2: Refactor `wait-database` with `wait_for_ready`**

Replace `wait-database` body (line ~322) with:

```makefile
wait-database:
	$(call log_info,"Waiting for database to be ready...")
	@CONTAINER="$(INFRASTRUCTURE_POSTGRES_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-postgresql"; fi; \
	USER="$(INFRASTRUCTURE_POSTGRES_USER)"; \
	if [ -z "$$USER" ]; then USER="tmi_dev"; fi; \
	timeout=$${TIMEOUTS_DB_READY:-300}; \
	while [ $$timeout -gt 0 ]; do \
		if docker exec $$CONTAINER pg_isready -U $$USER >/dev/null 2>&1; then \
			echo -e "$(GREEN)[SUCCESS]$(NC) Database is ready!"; \
			break; \
		fi; \
		echo "⏳ Waiting for database... ($$timeout seconds remaining)"; \
		sleep 2; \
		timeout=$$((timeout - 2)); \
	done; \
	if [ $$timeout -le 0 ]; then \
		echo -e "$(RED)[ERROR]$(NC) Database failed to start within 300 seconds"; \
		exit 1; \
	fi
```

Note: `wait-database` uses shell-resolved config variables for the container name, so can't use `wait_for_ready` macro directly. Keep the pattern but it's already clean.

Replace `wait-test-database` body (line ~359) with macro (hardcoded values):

```makefile
wait-test-database:
	$(call log_info,"Waiting for test database to be ready...")
	@$(call wait_for_ready,docker exec tmi-postgresql-test pg_isready -U tmi_dev,300,Test database)
```

Replace `wait-process` body (line ~506) with macro:

```makefile
wait-process:
	$(call log_info,"Waiting for server to be ready on port $(SERVER_PORT)")
	@$(call wait_for_ready,curl -s http://localhost:$(SERVER_PORT)/,$${TIMEOUTS_SERVER_READY:-300},Server)
```

Note: `$(SERVER_PORT)` is a Make variable so it works in the macro call.

- [ ] **Step 3: Refactor `stop-process` with `kill_port` (line ~387)**

Replace the body of `stop-process` with:

```makefile
stop-process:
	$(call log_info,"Killing processes on port $(SERVER_PORT)")
	@$(call kill_port,$(SERVER_PORT))
```

- [ ] **Step 4: Refactor `stop-server` to use macros (line ~441)**

Replace the body of `stop-server` with:

```makefile
stop-server:
	$(call log_info,"Stopping server...")
	@# Layer 1: Kill server using PID file (if available)
	@if [ -f .server.pid ]; then \
		PID=$$(cat .server.pid 2>/dev/null || true); \
		$(call graceful_kill,$$PID); \
		rm -f .server.pid; \
	fi
	@# Layer 2: Kill any tmiserver processes by name (catches orphans)
	@SERVER_PIDS=$$(ps aux | grep '[b]in/tmiserver' | awk '{print $$2}' || true); \
	if [ -n "$$SERVER_PIDS" ]; then \
		for PID in $$SERVER_PIDS; do \
			$(call graceful_kill,$$PID); \
		done; \
	fi
	@# Layer 3: Kill anything still holding the port
	@$(call kill_port,$(SERVER_PORT))
	@# Verify port is free
	@TRIES=0; \
	while [ $$TRIES -lt 10 ]; do \
		if ! lsof -ti :$(SERVER_PORT) > /dev/null 2>&1; then \
			break; \
		fi; \
		TRIES=$$((TRIES + 1)); \
		sleep 0.5; \
	done; \
	if lsof -ti :$(SERVER_PORT) > /dev/null 2>&1; then \
		echo "ERROR: Port $(SERVER_PORT) is still in use after stop attempts:"; \
		lsof -i :$(SERVER_PORT); \
		exit 1; \
	fi
	$(call log_success,"Server stopped")
```

- [ ] **Step 5: Refactor `kill-oauth-stub` to use `kill_port` (line ~1003)**

Replace the body with:

```makefile
kill-oauth-stub:
	$(call log_info,"Force killing anything on port 8079...")
	@$(call kill_port,8079)
	@rm -f .oauth-stub.pid
	$(call log_success,"Port 8079 cleared")
```

Note: `stop-oauth-stub` (spec Section 7a) is intentionally NOT refactored with `graceful_kill` — its multi-step shutdown (magic URL → SIGTERM → SIGKILL) doesn't map to the simple macro pattern. Kept as-is.

- [ ] **Step 6: Refactor `clean-process` to compose from targets (line ~607)**

Replace the entire `clean-process` body with:

```makefile
clean-process: stop-server stop-oauth-stub
```

Remove the old 55-line implementation.

- [ ] **Step 7: Verify**

Run: `make -n help >/dev/null 2>&1 && echo "OK"`
Expected: `OK`

Run: `make -n stop-server >/dev/null 2>&1 && echo "OK"`
Expected: `OK`

- [ ] **Step 8: Commit**

```bash
git add Makefile
git commit -m "refactor(make): apply reusable macros to infrastructure and process targets"
```

---

### Task 3: Remove dead code from Makefile

Remove deprecated targets, redundant aliases, unused targets, and the empty section.

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Remove deprecated CATS targets (lines ~1062-1070)**

Delete `cats-fuzz-prep` (lines 1062-1065) and `cats-set-max-quotas` (lines 1067-1070).

- [ ] **Step 2: Remove redundant aliases from backward-compat section (lines ~1514-1523)**

Remove `build-everything: build-server` (line 1518).
Remove `clean-dev` target (lines 866-868).
Keep `build`, `test`, `lint`, `clean`, `dev` aliases.
Keep `start-service` and `stop-service` aliases.

- [ ] **Step 3: Remove `execute-tests-unit` (lines ~530-551)**

Delete the entire `execute-tests-unit` target and its `.PHONY` declaration.

- [ ] **Step 4: Remove `check-syft` (lines ~1689-1698)**

Delete the entire `check-syft` target.

- [ ] **Step 5: Remove empty DISTROLESS section (lines ~1505-1508)**

Delete:
```
# ============================================================================
# DISTROLESS CONTAINER MANAGEMENT
# ============================================================================
```

- [ ] **Step 6: Remove `test-api-oci` (lines ~806-808)**

Delete the target.

- [ ] **Step 7: Remove `start-dev-0` (lines ~834-840)**

Delete the target.

- [ ] **Step 8: Remove `generate-sbom-all` (lines ~1722-1729) and `build-server-sbom` (line ~1735)**

Delete both targets. Modify `generate-sbom` to accept `ALL=true`:

```makefile
generate-sbom: check-cyclonedx
	$(call log_info,Generating SBOM for Go application...)
	@mkdir -p security-reports/sbom
	@cyclonedx-gomod app -json -output security-reports/sbom/tmi-server-$(VERSION)-sbom.json -main cmd/server
	@cyclonedx-gomod app -output security-reports/sbom/tmi-server-$(VERSION)-sbom.xml -main cmd/server
	$(call log_success,SBOM generated: security-reports/sbom/tmi-server-$(VERSION)-sbom.json)
	$(call log_success,SBOM generated: security-reports/sbom/tmi-server-$(VERSION)-sbom.xml)
	@if [ "$(ALL)" = "true" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Generating module SBOMs..."; \
		cyclonedx-gomod mod -json -output security-reports/sbom/tmi-module-$(VERSION)-sbom.json; \
		cyclonedx-gomod mod -output security-reports/sbom/tmi-module-$(VERSION)-sbom.xml; \
		echo -e "$(GREEN)[SUCCESS]$(NC) All Go SBOMs generated in security-reports/sbom/"; \
	fi
```

- [ ] **Step 9: Remove `arazzo-all` (line ~1772)**

Delete: `arazzo-all: arazzo-install generate-arazzo`

- [ ] **Step 10: Remove 5 CATS variant targets (lines ~1092-1135)**

Delete `cats-fuzz-user`, `cats-fuzz-server`, `cats-fuzz-custom`, `cats-fuzz-path`, `cats-fuzz-full`.

- [ ] **Step 11: Update `.PHONY` declarations**

Remove all deleted targets from `.PHONY` lines:
- Line ~670: remove `test-api-oci`, `start-dev-0`, `clean-dev`
- Line ~1044: remove `cats-fuzz-prep`, `cats-set-max-quotas`, `cats-fuzz-user`, `cats-fuzz-server`, `cats-fuzz-custom`, `cats-fuzz-path`, `cats-fuzz-full`
- Line ~530: remove `execute-tests-unit`
- Line ~1514: remove `build-everything`
- Line ~1674: remove `check-syft`, `generate-sbom-all`, `build-server-sbom`
- Line ~1741: remove `arazzo-all`

- [ ] **Step 12: Verify**

Run: `make -n help >/dev/null 2>&1 && echo "OK"`
Expected: `OK`

- [ ] **Step 13: Commit**

```bash
git add Makefile
git commit -m "refactor(make): remove dead code — deprecated, redundant, and unused targets"
```

---

### Task 4: Consolidate CATS targets and fix bugs

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Rewrite `cats-fuzz` to accept variables (line ~1072)**

Replace the existing `cats-fuzz` target with:

```makefile
# Usage:
#   make cats-fuzz                                       # defaults (charlie, localhost:8080)
#   make cats-fuzz FUZZ_USER=alice                       # custom user
#   make cats-fuzz FUZZ_SERVER=http://host:8080          # custom server
#   make cats-fuzz ENDPOINT=/addons                      # specific endpoint
#   make cats-fuzz BLACKBOX=true                         # blackbox mode
#   make cats-fuzz FUZZ_USER=alice ENDPOINT=/addons      # combine any options
cats-fuzz: cats-seed  ## Run CATS API fuzzing with database-agnostic seeding
	$(call log_info,"Running CATS API fuzzing with OAuth authentication...")
	@if ! command -v cats >/dev/null 2>&1; then \
		$(call log_error,"CATS tool not found. Please install it first."); \
		$(call log_info,"See: https://github.com/Endava/cats"); \
		$(call log_info,"On MacOS with Homebrew: brew install cats"); \
		exit 1; \
	fi
	@ARGS=""; \
	if [ -n "$(FUZZ_USER)" ]; then ARGS="$$ARGS -u $(FUZZ_USER)"; fi; \
	if [ -n "$(FUZZ_SERVER)" ]; then ARGS="$$ARGS -s $(FUZZ_SERVER)"; fi; \
	if [ -n "$(ENDPOINT)" ]; then ARGS="$$ARGS -p $(ENDPOINT)"; fi; \
	if [ "$(BLACKBOX)" = "true" ]; then ARGS="$$ARGS -b"; fi; \
	./scripts/run-cats-fuzz.sh $$ARGS
```

- [ ] **Step 2: Rewrite `cats-fuzz-oci` similarly (line ~1082)**

```makefile
cats-fuzz-oci: cats-seed-oci  ## Run CATS API fuzzing with OCI Autonomous Database
	$(call log_info,"Running CATS API fuzzing with OCI ADB...")
	@if ! command -v cats >/dev/null 2>&1; then \
		$(call log_error,"CATS tool not found. Please install it first."); \
		$(call log_info,"See: https://github.com/Endava/cats"); \
		$(call log_info,"On MacOS with Homebrew: brew install cats"); \
		exit 1; \
	fi
	@ARGS=""; \
	if [ -n "$(FUZZ_USER)" ]; then ARGS="$$ARGS -u $(FUZZ_USER)"; fi; \
	if [ -n "$(FUZZ_SERVER)" ]; then ARGS="$$ARGS -s $(FUZZ_SERVER)"; fi; \
	if [ -n "$(ENDPOINT)" ]; then ARGS="$$ARGS -p $(ENDPOINT)"; fi; \
	if [ "$(BLACKBOX)" = "true" ]; then ARGS="$$ARGS -b"; fi; \
	./scripts/run-cats-fuzz.sh $$ARGS
```

- [ ] **Step 3: Fix `deploy-heroku` color variables (line ~1554)**

Replace undefined `$(COLOR_BLUE)`, `$(COLOR_GREEN)`, `$(COLOR_RESET)` with `$(call log_info,...)` / `$(call log_success,...)`:

```makefile
deploy-heroku:
	$(call log_info,"Starting Heroku deployment...")
	$(call log_info,"Building server binary...")
	@$(MAKE) build-server
	$(call log_info,"Checking git status...")
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Committing changes..."; \
		git add -A; \
		git commit -m "chore: Build and deploy to Heroku [skip ci]" || true; \
	else \
		echo -e "$(BLUE)[INFO]$(NC) No changes to commit"; \
	fi
	$(call log_info,"Pushing to GitHub main branch...")
	@git push origin main
	$(call log_info,"Pushing to Heroku...")
	@git push heroku main
	$(call log_success,"Deployment complete!")
	$(call log_info,"Checking deployment status...")
	@heroku releases --app tmi-server | head -3
```

- [ ] **Step 4: Fix TF_ENV default (line ~1386)**

Change: `TF_ENV ?= oci-production` → `TF_ENV ?= oci-public`

Change line ~1398 comment: `(TF_ENV=oci-production)` → `(TF_ENV=oci-public)`

Change line ~1447: `deploy-oci: TF_ENV=oci-production` → `deploy-oci: TF_ENV=oci-public`

Change line ~1451: `deploy-oci-plan: TF_ENV=oci-production` → `deploy-oci-plan: TF_ENV=oci-public`

- [ ] **Step 5: Remove `Dockerfile.dev` block from `scan-containers` (lines ~1279-1285)**

Delete the block:
```makefile
	@if [ -f "Dockerfile.dev" ]; then \
		echo "Building and scanning application image..."; \
		docker build -f Dockerfile.dev -t tmi-temp-scan:latest . >/dev/null 2>&1 || true; \
		grype tmi-temp-scan:latest -o sarif > security-reports/application-scan.sarif 2>/dev/null || true; \
		grype tmi-temp-scan:latest -o table > security-reports/application-scan.txt 2>&1 || true; \
		docker rmi tmi-temp-scan:latest >/dev/null 2>&1 || true; \
	fi
```

- [ ] **Step 6: Fix `test-coverage` trap (line ~873)**

Change only the trap (line 873):
`@trap '$(MAKE) -f $(MAKEFILE_LIST) clean-everything' EXIT; \`
→ `@trap '$(MAKE) -f $(MAKEFILE_LIST) clean-test-infrastructure' EXIT; \`

Leave line 874 (`clean-everything` before the test run) as-is — the spec only addresses the trap, not the pre-test cleanup.

- [ ] **Step 7: Verify**

Run: `make -n help >/dev/null 2>&1 && echo "OK"`
Expected: `OK`

- [ ] **Step 8: Commit**

```bash
git add Makefile
git commit -m "fix(make): consolidate CATS targets, fix color vars, TF_ENV default, Dockerfile.dev ref, test-coverage trap"
```

---

### Task 5: Naming consistency and test-framework.mk updates

**Files:**
- Modify: `scripts/test-framework.mk`

- [ ] **Step 1: Rename `test-clean` to `clean-test-outputs` and remove `test-outputs-clean-integration`**

Rename the target at line ~122:
```makefile
# Clean all test outputs
clean-test-outputs:
	$(call log_info,"Cleaning test outputs...")
	@rm -rf test/outputs/integration/*
	@rm -rf test/outputs/unit/*
	@rm -rf test/outputs/cats/*
	@rm -rf test/outputs/newman/*
	@rm -rf test/outputs/security/*
	@rm -rf test/outputs/wstest/*
	$(call log_success,"Test outputs cleaned")

# Backward-compat alias
test-clean: clean-test-outputs
	$(call log_warning,"'test-clean' is deprecated. Use 'clean-test-outputs'.")
```

Delete the `test-outputs-clean-integration` target (lines ~134-137).

- [ ] **Step 2: Rename `test-framework-check` to `check-test-framework`**

Rename the target at line ~140 and fix the OpenAPI spec path:

```makefile
# Validate integration test framework setup
check-test-framework:
	$(call log_info,"Validating integration test framework...")
	@echo -e "$(BLUE)[INFO]$(NC) Checking directory structure..."
	@test -d test/integration/framework || (echo -e "$(RED)[ERROR]$(NC) test/integration/framework missing" && exit 1)
	@test -d test/integration/workflows || (echo -e "$(RED)[ERROR]$(NC) test/integration/workflows missing" && exit 1)
	@test -d test/integration/spec || (echo -e "$(RED)[ERROR]$(NC) test/integration/spec missing" && exit 1)
	@test -d test/outputs/integration || mkdir -p test/outputs/integration
	@echo -e "$(BLUE)[INFO]$(NC) Checking OAuth stub..."
	@if ! pgrep -f "oauth-client-callback-stub.py" > /dev/null; then \
		echo -e "$(YELLOW)[WARNING]$(NC) OAuth stub not running (run 'make start-oauth-stub')"; \
	else \
		echo -e "$(GREEN)[OK]$(NC) OAuth stub is running"; \
	fi
	@echo -e "$(BLUE)[INFO]$(NC) Checking server..."
	@if ! curl -s http://localhost:8080/ > /dev/null 2>&1; then \
		echo -e "$(YELLOW)[WARNING]$(NC) Server not running (run 'make start-dev')"; \
	else \
		echo -e "$(GREEN)[OK]$(NC) Server is running"; \
	fi
	@echo -e "$(BLUE)[INFO]$(NC) Checking OpenAPI spec..."
	@test -f api-schema/tmi-openapi.json || (echo -e "$(RED)[ERROR]$(NC) OpenAPI spec missing" && exit 1)
	@echo -e "$(GREEN)[SUCCESS]$(NC) Integration test framework setup validated"

# Backward-compat alias
test-framework-check: check-test-framework
	$(call log_warning,"'test-framework-check' is deprecated. Use 'check-test-framework'.")
```

- [ ] **Step 3: Update `.PHONY` declarations in test-framework.mk**

Change line 9 (note: the existing line has `test-outputs-clean` which doesn't match any target — pre-existing bug, fixed by this replacement):
```makefile
.PHONY: test-integration-new test-integration-workflow test-integration-quick clean-test-outputs test-clean check-test-framework test-framework-check
```

- [ ] **Step 4: Update `test-help` target**

Replace the help content (lines ~163-200) with updated names:

```makefile
test-help:
	@echo "Integration Test Framework Commands:"
	@echo ""
	@echo "  Setup:"
	@echo "    make check-test-framework   - Validate framework setup"
	@echo "    make start-oauth-stub       - Start OAuth callback stub"
	@echo ""
	@echo "  Running Tests (Tier-Based):"
	@echo "    make test-integration-tier1  - Tier 1: Core workflows (< 2 min, 36 ops)"
	@echo "    make test-integration-tier2  - Tier 2: Feature tests (< 10 min, 105 ops)"
	@echo "    make test-integration-tier3  - Tier 3: Edge cases & admin (< 15 min, 33 ops)"
	@echo "    make test-integration-all    - Run all tiers (< 27 min, 174 ops - 100%)"
	@echo ""
	@echo "  Running Tests (Other):"
	@echo "    make test-integration-new    - Run all integration tests"
	@echo "    make test-integration-quick  - Run quick example test"
	@echo "    make test-integration-full   - Full setup + run all tests"
	@echo "    make test-integration-workflow WORKFLOW=name - Run specific workflow"
	@echo ""
	@echo "  Tier Descriptions:"
	@echo "    Tier 1: OAuth, ThreatModel CRUD, Diagram CRUD, User operations"
	@echo "    Tier 2: Metadata, Assets, Documents, Webhooks, Addons, SAML, etc."
	@echo "    Tier 3: Admin operations, Authorization, Pagination, Error handling"
	@echo ""
	@echo "  Examples:"
	@echo "    make test-integration-tier1"
	@echo "    make test-integration-workflow WORKFLOW=OAuthFlow"
	@echo "    make test-integration-workflow WORKFLOW=ThreatModelCRUD"
	@echo ""
	@echo "  Cleanup:"
	@echo "    make clean-test-outputs     - Clean all test outputs"
	@echo ""
	@echo "  Documentation:"
	@echo "    cat test/integration/README.md"
```

- [ ] **Step 5: Verify**

Run: `make -n clean-test-outputs >/dev/null 2>&1 && echo "OK"`
Expected: `OK`

Run: `make -n check-test-framework >/dev/null 2>&1 && echo "OK"`
Expected: `OK`

- [ ] **Step 6: Commit**

```bash
git add scripts/test-framework.mk
git commit -m "refactor(make): rename test-clean and test-framework-check for naming consistency"
```

---

### Task 6: Update `help` target and final cleanup

**Files:**
- Modify: `Makefile` — `help` target (lines ~1919-2018)

- [ ] **Step 1: Rewrite the `help` target**

Update the help text to reflect all changes. Key changes:
- Remove references to all deleted targets
- Add variable-based usage for `cats-fuzz`
- Remove `config/coverage-report.yml` line
- Update SBOM section
- Update Arazzo section
- Update Terraform default

The full replacement for the help target:

```makefile
help:
	@echo "TMI Makefile"
	@echo ""
	@echo "Usage: make <target> [VARIABLE=value ...]"
	@echo ""
	@echo "Core Targets:"
	@echo "  status                 - Check status of all services"
	@echo "  test-unit              - Run unit tests"
	@echo "  test-integration       - Run integration tests with PostgreSQL (default)"
	@echo "  test-integration-pg    - Run integration tests with PostgreSQL (Docker)"
	@echo "  test-integration-oci   - Run integration tests with Oracle ADB"
	@echo "  test-api               - Run comprehensive Postman/Newman API tests"
	@echo "  start-dev              - Start development environment"
	@echo "  start-dev-oci          - Start dev environment with OCI Autonomous Database"
	@echo "  reset-db-oci           - Drop all tables in OCI ADB (destructive)"
	@echo ""
	@echo "CATS Fuzzing:"
	@echo "  cats-fuzz              - Run CATS API fuzzing (PostgreSQL)"
	@echo "  cats-fuzz-oci          - Run CATS API fuzzing (Oracle ADB)"
	@echo "    Variables: FUZZ_USER=alice FUZZ_SERVER=http://host ENDPOINT=/path BLACKBOX=true"
	@echo "  cats-seed              - Seed database for CATS (PostgreSQL)"
	@echo "  cats-seed-oci          - Seed database for CATS (Oracle ADB)"
	@echo "  analyze-cats-results   - Parse and query CATS results"
	@echo ""
	@echo "Container Management:"
	@echo "  build-container-db           - Build PostgreSQL container"
	@echo "  build-container-redis        - Build Redis container"
	@echo "  build-container-tmi          - Build TMI server container"
	@echo "  build-container-oracle       - Build TMI container with Oracle ADB support"
	@echo "  build-container-oracle-push  - Build and push Oracle container to OCI"
	@echo "  build-container-redis-oracle - Build Redis container on Oracle Linux"
	@echo "  build-container-redis-oracle-push - Build and push Redis Oracle to OCI"
	@echo "  build-containers-oracle      - Build all Oracle Linux containers"
	@echo "  build-containers-oracle-push - Build and push all Oracle containers"
	@echo "  build-containers             - Build all containers (db, redis, tmi)"
	@echo "  scan-containers              - Scan containers for vulnerabilities"
	@echo "  report-containers            - Generate security report"
	@echo "  start-containers-environment - Start development with containers"
	@echo "  build-containers-all         - Build and report"
	@echo ""
	@echo "Multi-Architecture Container Builds (amd64 + arm64):"
	@echo "  build-container-tmi-multiarch       - Build and push TMI server multi-arch image"
	@echo "  build-container-redis-multiarch     - Build and push Redis multi-arch image"
	@echo "  build-containers-multiarch          - Build and push all multi-arch images"
	@echo "  build-container-tmi-multiarch-local - Build TMI server for local platform only"
	@echo "  build-container-redis-multiarch-local - Build Redis for local platform only"
	@echo "  build-containers-multiarch-local    - Build all images for local platform only"
	@echo ""
	@echo "OCI Functions (Certificate Manager):"
	@echo "  fn-build-certmgr             - Build the certificate manager function"
	@echo "  fn-deploy-certmgr            - Deploy certificate manager to OCI"
	@echo "  fn-invoke-certmgr            - Invoke certificate manager for testing"
	@echo "  fn-logs-certmgr              - View certificate manager logs"
	@echo ""
	@echo "Terraform Infrastructure Management:"
	@echo "  tf-init                      - Initialize Terraform (TF_ENV=oci-public)"
	@echo "  tf-validate                  - Validate Terraform configuration"
	@echo "  tf-fmt                       - Format all Terraform files"
	@echo "  tf-plan                      - Plan infrastructure changes"
	@echo "  tf-apply                     - Apply infrastructure changes"
	@echo "  tf-apply-plan                - Apply from saved plan file"
	@echo "  tf-output                    - Show Terraform outputs"
	@echo "  tf-destroy                   - Destroy infrastructure (DESTRUCTIVE!)"
	@echo "  deploy-oci                   - Deploy TMI to OCI"
	@echo "  deploy-oci-plan              - Plan TMI OCI deployment"
	@echo ""
	@echo "SBOM Generation (Software Bill of Materials):"
	@echo "  generate-sbom                - Generate SBOM for Go application (cyclonedx-gomod)"
	@echo "    Variables: ALL=true to also generate module SBOMs"
	@echo "  build-with-sbom              - Build server and generate SBOM"
	@echo "  check-cyclonedx              - Verify cyclonedx-gomod is installed"
	@echo ""
	@echo "Atomic Components (building blocks):"
	@echo "  start-database         - Start PostgreSQL container"
	@echo "  start-redis            - Start Redis container"
	@echo "  build-server           - Build server binary"
	@echo "  migrate-database       - Run database migrations"
	@echo "  reset-database         - Drop database and run migrations (DESTRUCTIVE)"
	@echo "  start-server           - Start server"
	@echo "  clean-everything       - Clean up everything"
	@echo ""
	@echo "WebSocket Testing:"
	@echo "  build-wstest           - Build WebSocket test harness"
	@echo "  wstest                 - Run WebSocket test with 3 terminals"
	@echo "  monitor-wstest         - Run WebSocket monitor"
	@echo "  clean-wstest           - Stop all WebSocket test instances"
	@echo ""
	@echo "Arazzo Workflow Generation:"
	@echo "  generate-arazzo        - Generate Arazzo workflow specifications"
	@echo "  validate-arazzo        - Validate generated Arazzo specifications"
	@echo "  arazzo-scaffold        - Generate base scaffold from OpenAPI"
	@echo "  arazzo-enhance         - Enhance scaffold with TMI workflow data"
	@echo "  arazzo-install         - Install Arazzo tooling"
	@echo ""
	@echo "Validation Targets:"
	@echo "  validate-openapi       - Validate OpenAPI specification"
	@echo "  validate-asyncapi      - Validate AsyncAPI specification"
	@echo ""
	@echo "Configuration Files:"
	@echo "  config/test-unit.yml           - Unit testing configuration"
	@echo "  config/test-integration.yml    - Integration testing configuration"
	@echo "  config/dev-environment.yml     - Development environment configuration"
	@echo ""
```

- [ ] **Step 2: Verify all targets parse**

Run: `make help`
Expected: Clean help output with no undefined variable warnings

Run: `make list-targets | wc -l`
Expected: Fewer targets than before (sanity check)

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "refactor(make): update help target to reflect all cleanup changes"
```

---

### Task 7: Final verification and spec commit

- [ ] **Step 1: Run `make -n` on key targets to verify they parse**

```bash
make -n build-server >/dev/null 2>&1 && echo "build-server: OK"
make -n test-unit >/dev/null 2>&1 && echo "test-unit: OK"
make -n start-dev >/dev/null 2>&1 && echo "start-dev: OK"
make -n stop-server >/dev/null 2>&1 && echo "stop-server: OK"
make -n clean-everything >/dev/null 2>&1 && echo "clean-everything: OK"
make -n cats-fuzz >/dev/null 2>&1 && echo "cats-fuzz: OK"
make -n clean-test-outputs >/dev/null 2>&1 && echo "clean-test-outputs: OK"
make -n check-test-framework >/dev/null 2>&1 && echo "check-test-framework: OK"
make -n test-clean >/dev/null 2>&1 && echo "test-clean (compat): OK"
make -n test-framework-check >/dev/null 2>&1 && echo "test-framework-check (compat): OK"
make -n generate-sbom >/dev/null 2>&1 && echo "generate-sbom: OK"
make -n start-service >/dev/null 2>&1 && echo "start-service: OK"
```

- [ ] **Step 2: Run `make lint` and `make build-server`**

Per CLAUDE.md requirements, verify the build still works after Makefile changes.

- [ ] **Step 3: Run `make test-unit`**

Verify unit tests still pass.

- [ ] **Step 4: Commit spec document**

```bash
git add docs/superpowers/specs/2026-03-21-makefile-refactoring-design.md docs/superpowers/plans/2026-03-21-makefile-refactoring.md
git commit -m "docs: add Makefile refactoring spec and implementation plan"
```

- [ ] **Step 5: Push**

```bash
git pull --rebase && git push
```
