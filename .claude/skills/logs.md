# Server Logs Query Skill

You are a specialized agent for querying TMI server logs in both local development and Heroku production environments.

## Log Locations

### Local Development
- **Log file**: `logs/tmi.log`
- **Query method**: Use `grep`, `tail`, `head`, or other file tools
- **Format**: Structured JSON logging with timestamp, level, and contextual fields

### Heroku Production
- **Query method**: Use `heroku logs` command
- **App name**: Retrieved dynamically or specified by user
- **Format**: Heroku log stream with dyno prefix and structured JSON content

## Common Query Patterns

### Local Development Logs

```bash
# Tail recent logs
tail -100 logs/tmi.log

# Search for specific user activity
grep "alice@" logs/tmi.log

# Search for WebSocket operations
grep "diagram_operation" logs/tmi.log

# Search for operation rejections
grep "operation_rejected" logs/tmi.log

# Search for errors
grep "\"level\":\"ERROR\"" logs/tmi.log

# Time-based filtering (requires jq)
cat logs/tmi.log | jq 'select(.time > "2024-10-28T10:00:00Z")'

# Filter by session ID
grep "session_id.*abc123" logs/tmi.log
```

### Heroku Production Logs

```bash
# Tail recent logs (last 100 lines)
heroku logs --tail --num 100

# Search for specific user
heroku logs --tail | grep "alice@"

# Search for errors only
heroku logs --tail | grep "ERROR"

# Get logs from specific time range
heroku logs --since="1 hour ago"
heroku logs --since="2024-10-28T10:00:00Z"

# Filter by dyno/process type
heroku logs --dyno web --tail

# Search for WebSocket activity
heroku logs --tail | grep "diagram_operation"

# Search for operation rejections
heroku logs --tail | grep "operation_rejected"
```

## Diagnostic Scenarios

### Debugging Operation Rejection
When investigating why a diagram operation was rejected:

1. **Local**:
   ```bash
   grep -A 10 "operation_rejected" logs/tmi.log | tail -50
   ```

2. **Heroku**:
   ```bash
   heroku logs --tail | grep "operation_rejected"
   ```

Look for:
- `operation_id`: The UUID of the rejected operation
- `reason`: The rejection reason code
- `requires_resync`: Whether client needs to resync
- `affected_cells`: Which cells were affected

### Investigating WebSocket Issues

1. **Local**:
   ```bash
   grep "Session:" logs/tmi.log | grep "diagram_operation\|operation_rejected\|state_correction" | tail -50
   ```

2. **Heroku**:
   ```bash
   heroku logs --tail | grep "diagram_operation\|operation_rejected\|state_correction"
   ```

### Finding User Activity

1. **Local**:
   ```bash
   grep "User: alice@" logs/tmi.log | tail -50
   ```

2. **Heroku**:
   ```bash
   heroku logs --tail | grep "alice@"
   ```

### Checking Operation Flow

1. **Local** - Full operation lifecycle:
   ```bash
   grep "OperationID: <operation-uuid>" logs/tmi.log
   ```

2. **Heroku**:
   ```bash
   heroku logs | grep "<operation-uuid>"
   ```

## Advanced Queries

### Using jq for Structured Analysis (Local only)

```bash
# Extract all operation rejections with details
cat logs/tmi.log | jq 'select(.msg | contains("operation_rejected"))'

# Count rejections by reason
cat logs/tmi.log | jq 'select(.msg | contains("REJECTED")) | .reason' | sort | uniq -c

# Extract operation IDs that were rejected
cat logs/tmi.log | jq -r 'select(.msg | contains("REJECTED")) | .operation_id'
```

### Heroku App Detection

```bash
# List available apps
heroku apps

# Get current git remote
git remote -v | grep heroku

# Auto-detect app from git remote
heroku logs --app $(git remote -v | grep heroku | head -1 | sed 's/.*\/\(.*\)\.git.*/\1/')
```

## Task Execution Guidelines

When the user asks you to query logs:

1. **Determine Environment**: Ask if they want local or Heroku logs (if not specified)
2. **Understand Query Intent**: What are they looking for? (errors, specific user, operation rejections, etc.)
3. **Choose Appropriate Tool**: Use grep/tail for local, heroku logs for production
4. **Execute Query**: Run the appropriate command
5. **Parse Results**: Extract relevant information and present clearly
6. **Suggest Follow-ups**: If issues found, suggest next debugging steps

## Common Issues and Solutions

### Issue: Operation not broadcast, no feedback
**Solution**: Check for `operation_rejected` messages. If none found, this is the bug we're fixing.

```bash
# Local
grep "OperationID: <uuid>" logs/tmi.log | grep -E "REJECTED|operation_rejected"

# Heroku
heroku logs | grep "<uuid>" | grep -E "REJECTED|operation_rejected"
```

### Issue: Client thinks operation succeeded but server doesn't have it
**Solution**: Look for validation failures and check if rejection was sent:

```bash
# Local
grep "VALIDATION FAILED" logs/tmi.log | tail -20

# Heroku
heroku logs --tail | grep "VALIDATION FAILED"
```

### Issue: Need to see full operation lifecycle
**Solution**: Grep for operation ID across all log messages:

```bash
# Local
grep "OperationID.*<uuid>" logs/tmi.log | jq -c '{time, level, msg, operation_id, reason, applied}'

# Heroku
heroku logs | grep "<uuid>"
```

## Remember

- Always use structured logging field names when available
- For Heroku, consider log volume and use time filters (`--since`) for busy systems
- Local logs persist in `logs/tmi.log`, Heroku logs are ephemeral (use log drains for persistence)
- After implementing operation rejection feature, you should see `operation_rejected` messages in logs when operations fail
