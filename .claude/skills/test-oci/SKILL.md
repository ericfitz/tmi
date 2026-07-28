---
name: test-oci
description: Run the TMI comprehensive test suite against Oracle ADB (OCI) including unit tests, integration tests, API tests, and CATS security fuzzing. Use when asked to run tests against Oracle database.
allowed-tools: Bash, Read
---

# Comprehensive Test Suite Skill (Oracle ADB)

You are executing the TMI comprehensive test suite against Oracle Autonomous Database (OCI). This skill runs all test levels in sequence, stopping at the first failure to allow investigation.

## Prerequisites

Before running this skill, ensure:
1. Oracle Instant Client is installed
2. Wallet is extracted to `./wallet` directory
3. `scripts/oci-env.sh` exists with your OCI credentials (copy from `scripts/oci-env.sh.example`)

## Test Execution Order

The test suite runs in the following order, with each stage only running if the previous stage passed:

1. **Environment Setup** - Clean and rebuild the server with Oracle support
2. **Unit Tests** - Fast tests with no external dependencies (~5-10 seconds)
3. **Integration Tests (OCI)** - Full Oracle ADB integration tests (~30-60 seconds)
4. **API Tests** - Postman/Newman API test suite (~2 minutes)
5. **CATS Fuzzing (OCI)** - Security fuzzing with CATS (~9 minutes)

## Execution Instructions

### Step 0: Environment Setup

First, stop any running server and clean up, then rebuild with Oracle support and start fresh:

```bash
make dev-down
make clean-everything
```

Build the server with Oracle support:
```bash
. scripts/oci-env.sh && go build -tags oracle -o bin/tmiserver ./cmd/server/
```

Start the development environment with OCI:
```bash
make dev-up DB=oracle
```

Wait for the server to be fully ready before proceeding. You can verify with:
```bash
curl -s http://localhost:8080/ > /dev/null && echo "Server ready"
```

### Step 1: Unit Tests

Run unit tests (these don't use the database):
```bash
make test-unit
```

**Analysis**:
- Look for `PASS` or `FAIL` in the output
- Check the final line for `ok` (pass) or `FAIL` (failure)
- If any test fails, report the failing test name(s) and error message(s)
- **Stop here if any unit tests fail**

### Step 2: Integration Tests (OCI)

If unit tests passed, run integration tests against Oracle ADB:
```bash
make test-integration-oci
```

**Analysis**:
- Look for `PASS` or `FAIL` in the output
- Check for Oracle database connection errors
- If any test fails, report the failing test name(s) and error message(s)
- **Stop here if any integration tests fail**

### Step 3: API Tests

If integration tests passed, run API tests. The server should already be running from Step 0:
```bash
make test-api
```

**Analysis**:
- Look for Newman test results summary
- Check for `assertions` passed/failed counts
- Look for `iterations` and `requests` counts
- If any assertions fail, report the failing endpoint(s) and assertion(s)
- **Stop here if any API tests fail**

### Step 4: CATS Fuzzing (OCI)

If API tests passed, run CATS security fuzzing against the OCI-backed server:
```bash
make cats-fuzz-oci
```

This takes approximately 9 minutes. The output will show progress through various fuzzers.

### Step 5: Analyze CATS Results

`make cats-fuzz-oci` (Step 4) already parses and classifies results as part of the
run — there is no separate parse step. Query the results database directly. The cats
plugin is installed, so invoke the `/cats:report` skill for the full schema (tables,
views, worked queries) and for ad-hoc queries; `/cats:analyze` triages the findings.

For a quick shape-of-the-run check without loading a skill:

```bash
make query-cats-results
```

That target resolves the plugin the same way `/cats:*` does (installed copy first,
development checkout as fallback), so both run the same implementation.

**Analysis**:
- **False positives** are expected (e.g. 401/403 responses from auth tests) - these are NOT real issues
- Focus on the **error** and **warn** results in `true_positives_view` (`is_false_positive = 0`)
- Report any actual errors by path and fuzzer
- Warnings are less critical but should be noted

## Reporting Guidelines

### On Success
If all tests pass, report:
- Unit tests: X tests passed
- Integration tests (OCI): X tests passed
- API tests: X assertions passed
- CATS fuzzing (OCI): X tests run, Y errors (Z false positives excluded)

### On Failure
If any stage fails:
1. Clearly state which stage failed (unit/integration/API/CATS)
2. List the specific failing tests or endpoints
3. Include relevant error messages
4. Stop execution - do not proceed to later stages
5. Suggest next steps for investigation

## Important Notes

- **Oracle Support**: The server must be built with `-tags oracle` for OCI support. The `make dev-up DB=oracle` target handles this automatically.
- **CATS Seeding Tool**: Seeding runs through `bin/tmi-dbtool`, which must also be built with Oracle support. `make cats-seed-oci` (a prerequisite of `make cats-fuzz-oci`) calls `scripts/run-dbtool.py --oci`, which builds it with `-tags oracle` automatically after sourcing `scripts/oci-env.sh`. There is no separate build target, and `bin/cats-seed` is a pre-migration artifact that is no longer built or used.
- **False positives**: CATS will flag some responses (e.g. 401/403 from auth testing) as "errors" but these are expected. The rule set in `test/cats/false-positives.yaml` classifies these; matched rows are flagged via the `is_false_positive` column.
- **CATS duration**: The fuzzing stage takes ~9 minutes - this is normal
- **Server must be running**: All tests except unit tests require the dev server (`make dev-up DB=oracle`)
- **Redis required**: API and CATS tests require Redis (`make start-redis` - started automatically by `dev-up DB=oracle`)
- **No server restart during tests**: The API tests and CATS fuzzing run against the already-running server; they do not restart it

## Database Schema Reference

Invoke the `/cats:report` skill for the full results database schema — tables, views,
and worked queries.

## Troubleshooting

### Oracle Connection Issues
If you see Oracle connection errors:
1. Verify `scripts/oci-env.sh` has correct credentials
2. Check wallet is extracted to `./wallet`
3. Verify `DYLD_LIBRARY_PATH` points to Oracle Instant Client
4. Check `TNS_ADMIN` points to the wallet directory

### Server Fails to Start
Check the logs:
```bash
tail -50 logs/tmi.log
```

Common issues:
- Missing Oracle environment variables
- Wallet not found
- Database user not created in OCI ADB
