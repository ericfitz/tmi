---
name: monitor_500s
description: Monitor TMI server logs for HTTP 500 errors, investigate root causes, and file GitHub issues for distinct bugs. Scans logs/tmi.log for new 500 errors not previously investigated.
allowed-tools: Bash, Read, Glob, Grep, Agent
---

# Monitor 500 Errors Skill

You are a specialized agent that monitors TMI server logs for HTTP 500 errors, investigates their root causes by analyzing source code, and files GitHub issues with detailed analysis and fix recommendations.

## Prerequisites

- TMI dev server should be running (or have been running recently) so `logs/tmi.log` exists
- GitHub CLI (`gh`) must be authenticated

## Step 1: Load Investigation State

Read the investigation state file to determine what has already been investigated:

```bash
cat .claude/skills/monitor_500s/investigated.json 2>/dev/null || echo '{"last_scan_timestamp":"1970-01-01T00:00:00Z","investigated_signatures":{}}'
```

Parse the `last_scan_timestamp` to know where to start scanning.

## Step 2: Extract 500 Errors from Logs

The log file is at `logs/tmi.log`. The format depends on the server mode:

**Development mode** (slog TextHandler - key=value format):
```
time=2026-03-08T10:00:00.000-07:00 level=ERROR msg="Request completed with server error" request_id=<uuid> client_ip=127.0.0.1 user_id=alice method=POST path=/api/threat-models status_code=500 duration=1.234s response_size=150
time=2026-03-08T10:00:00.000-07:00 level=ERROR msg="Panic recovered" request_id=<uuid> client_ip=127.0.0.1 user_id=alice panic_value="runtime error: index out of range" stack_trace="goroutine 1..." method=POST path=/api/threat-models
```

**Production mode** (slog JSONHandler - JSON format):
```json
{"time":"2026-03-08T10:00:00Z","level":"ERROR","msg":"Request completed with server error","request_id":"uuid","client_ip":"127.0.0.1","user_id":"alice","method":"POST","path":"/api/threat-models","status_code":500,"duration":1234000000,"response_size":150}
```

### Detection approach

1. Check if `logs/tmi.log` exists. If not, report "No log file found at logs/tmi.log. Is the dev server running?" and stop.

2. Detect format by checking the first non-empty line:
   - Starts with `{` → JSON format
   - Starts with `time=` → Text format

3. Extract 500 error entries since `last_scan_timestamp`:

For **text format**:
```bash
grep -E 'level=ERROR.*msg="(Request completed with server error|Panic recovered)"' logs/tmi.log
```

For **JSON format**:
```bash
jq -c 'select(.level == "ERROR" and (.msg == "Request completed with server error" or .msg == "Panic recovered"))' logs/tmi.log
```

4. Filter to only entries with timestamps after `last_scan_timestamp`. For text format, extract `time=` values and compare. For JSON, use jq `select(.time > "TIMESTAMP")`.

5. Parse each matching line to extract these fields:
   - `time` - timestamp
   - `msg` - either "Request completed with server error" or "Panic recovered"
   - `method` - HTTP method
   - `path` - request URL path
   - `status_code` - HTTP status code (500)
   - `request_id` - UUID for the request
   - `client_ip` - client IP address
   - `user_id` - authenticated user (may be `<nil>`)
   - `duration` - request duration (server error only)
   - `panic_value` - panic value (panic only)
   - `stack_trace` - stack trace string (panic only)

## Step 3: Group and Deduplicate

1. **Normalize paths**: Replace UUID segments with `{id}`:
   - Pattern: segments matching `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`
   - Example: `/api/threat-models/550e8400-e29b-41d4-a716-446655440000/diagrams` → `/api/threat-models/{id}/diagrams`

2. **Compute error signature** for each entry:
   - For panics: `{METHOD}:{normalized_path}:panic:{first_80_chars_of_panic_value}`
   - For non-panics: `{METHOD}:{normalized_path}:error`

3. **Group entries** by signature. For each group, track:
   - All timestamps (first_seen, last_seen)
   - Count of occurrences
   - All request_ids (up to 10)
   - Sample user_ids

4. **Filter out** signatures already present in `investigated_signatures` from the loaded state.

5. If no new signatures remain, report:
   > No new HTTP 500 errors since last scan at {last_scan_timestamp}.

   Update `last_scan_timestamp` to current time in investigated.json and stop.

## Step 4: Investigate Each New Error Group

For each new error signature group, perform a root cause investigation:

### 4a. Identify the handler

1. Determine the API endpoint from the method + path pattern.
2. Search for the route registration:
   ```
   Grep for the path pattern in api/server.go and api/api.go
   ```
3. Find the handler function implementation. Read the handler code.

### 4b. Analyze the error

**For panics** (msg="Panic recovered"):
1. Parse the `stack_trace` field to identify the crash location (file:line)
2. Read the crashing code
3. Identify what triggered the panic (nil pointer, index out of range, etc.)
4. Check for missing nil checks, bounds checks, or error handling

**For non-panic 500s** (msg="Request completed with server error"):
1. Read the handler and trace through error paths
2. Check `api/request_utils.go` — `HandleRequestError()` (line ~437) and `StoreErrorToRequestError()` (line ~587) for how errors become 500s
3. Check database/store operations used by the handler
4. Look for: unhandled errors, missing error type assertions, database connection issues

### 4c. Check recent changes

```bash
git log --oneline -10 -- <affected_file>
```

Look for recent commits that may have introduced the bug.

### 4d. Formulate fix recommendation

Based on the investigation, write a specific fix recommendation including:
- Which file and function to modify
- What the fix should be (e.g., "add nil check before accessing field X")
- Whether this is a data issue, code bug, or infrastructure problem

## Step 5: Present Findings and File Issues

For **each** new error group, present the findings to the user:

### Present findings

Display a summary like:

```
## New 500 Error: {signature}

**Occurrences**: {count} times between {first_seen} and {last_seen}
**Sample request IDs**: {request_id_1}, {request_id_2}, ...

### Root Cause Analysis
{description of what's happening and why}

### Affected Code
- {file_path}:{line_number} - {brief description}

### Stack Trace (if panic)
{sanitized stack trace, max 10 lines}

### Suggested Fix
{specific fix recommendation}
```

Then ask the user: **"Would you like me to file a GitHub issue for this?"**

### If user confirms, file the issue

1. Check for existing open issues:
   ```bash
   gh issue list --repo ericfitz/tmi --state open --label "500-error" --search "{method} {path_pattern}" --json number,title
   ```

2. If a matching issue exists, add a comment:
   ```bash
   gh issue comment {issue_number} --repo ericfitz/tmi --body "$(cat <<'COMMENT_EOF'
   ## Additional Occurrences Detected

   **Count**: {count} new occurrences
   **Period**: {first_seen} to {last_seen}
   **Sample Request IDs**: {request_ids}

   No change in root cause analysis.
   COMMENT_EOF
   )"
   ```

3. If no existing issue, get context and create one:

   ```bash
   # Get current branch and check for milestone
   BRANCH=$(git branch --show-current)
   MILESTONE=$(gh api repos/ericfitz/tmi/milestones --jq ".[] | select(.title == \"$BRANCH\") | .title" 2>/dev/null)
   ```

   ```bash
   gh issue create --repo ericfitz/tmi \
     --title "500: {METHOD} {normalized_path} - {error_summary}" \
     --label "bug" --label "500-error" \
     [--milestone "$MILESTONE" if matching milestone exists] \
     --body "$(cat <<'ISSUE_EOF'
   ## 500 Error Report

   ### Summary
   {one-line description of the error}

   ### Error Signature
   - **Method**: {METHOD}
   - **Path Pattern**: {normalized_path}
   - **Type**: {panic or server error}

   ### Occurrences
   - **Count**: {count}
   - **First Seen**: {first_seen}
   - **Last Seen**: {last_seen}
   - **Sample Request IDs**: {request_ids}

   ### Root Cause Analysis
   {detailed analysis of what's going wrong, referencing specific code}

   ### Stack Trace
   {sanitized stack trace if panic, or "N/A" for non-panic errors}

   ### Affected Code
   - `{file_path}:{line_number}` - {description}

   ### Suggested Fix
   {specific remediation steps}

   ### Log Evidence
   ```
   {sanitized log entries - IPs replaced with [redacted], tokens removed}
   ```

   ---
   *Detected by monitor_500s skill*
   ISSUE_EOF
   )"
   ```

4. Add the issue to the TMI project:
   ```bash
   ISSUE_URL="<url returned by gh issue create>"
   ITEM_ID=$(gh project item-add 2 --owner ericfitz --url "$ISSUE_URL" --format json | jq -r '.id')
   gh project item-edit --project-id PVT_kwHOACjZhM4BC0Z1 --id "$ITEM_ID" --field-id PVTSSF_lAHOACjZhM4BC0Z1zg06000 --single-select-option-id 47fc9ee4
   ```

### If user declines, skip filing but still record

Record the signature in investigated.json with `"github_issue": null` to avoid re-investigating.

## Step 6: Update Investigation State

After processing all new error groups, update `.claude/skills/monitor_500s/investigated.json`:

```json
{
  "last_scan_timestamp": "<current ISO 8601 timestamp>",
  "investigated_signatures": {
    "<existing entries>": "...",
    "<new signature>": {
      "first_seen": "<first occurrence timestamp>",
      "last_seen": "<last occurrence timestamp>",
      "count": <number of occurrences>,
      "github_issue": <issue number or null if declined>,
      "request_ids": ["<up to 10 request IDs>"]
    }
  }
}
```

Write the file using the Write tool or jq.

## Step 7: Report Summary

Provide a final summary:

```
## 500 Error Monitor Summary

- **Scan period**: {last_scan_timestamp} to {current_time}
- **New error groups found**: {count}
- **Issues filed**: {count} (list URLs)
- **Issues updated**: {count} (list URLs)
- **Declined**: {count}
- **Previously investigated (skipped)**: {count}
```

## Security Requirements

When including log evidence or stack traces in GitHub issues:

- **ALWAYS** replace `client_ip` values with `[redacted]`
- **ALWAYS** remove any Authorization headers, tokens, cookies, or secrets
- **ALWAYS** strip filesystem paths to be relative to project root (remove `/Users/...` prefixes)
- **NEVER** include request or response bodies that might contain user data
- **NEVER** include full `user_id` values in public issues — use only first name or initials

## Key Source Files for Investigation

These files contain the 500 error generation and handling logic:

- `internal/slogging/middleware.go` - LoggerMiddleware (lines 55-65: server error logging), Recoverer (lines 96-103: panic logging)
- `api/recovery_middleware.go` - CustomRecoveryMiddleware (panic catch, stack trace, error response)
- `api/request_utils.go` - HandleRequestError (~line 437), StoreErrorToRequestError (~line 587)
- `api/server.go` - Route registration, handler mapping
- `internal/slogging/context.go` - WithContext (lines 94-126: request_id, client_ip, user_id fields)
- `internal/slogging/logger.go` - TextHandler (dev) vs JSONHandler (prod) at line 232-237

## Periodic Execution

To run this skill periodically during a development session:

```
/loop 10m /monitor_500s
```

This checks for new 500 errors every 10 minutes. The loop is session-scoped and stops when the Claude Code session ends.
