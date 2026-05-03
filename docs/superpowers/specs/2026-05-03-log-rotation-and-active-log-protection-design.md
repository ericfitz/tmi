# Log rotation config + active-log deletion protection — Design

**Issue:** [#372](https://github.com/ericfitz/tmi/issues/372)
**Date:** 2026-05-03
**Branch target:** `dev/1.4.0`

## Summary

Two distinct improvements bundled in one issue:

1. **Active-log deletion protection.** Any code path that deletes `logs/tmi.log` while the TMI server is running silently disables logging from the operator's point of view (the server keeps writing to an unlinked inode that is no longer reachable from the filesystem). This is fixed in two places: the server detects the deletion via `fsnotify` and reopens the file; cleanup scripts that touch `logs/` first stop any running server.
2. **Cloud-sink watchdog.** Cloud log sinks (`CloudLogWriter`) report errors via internal counters, but those counters are never surfaced. A new component watches both health-check transitions and consecutive-write-failure thresholds and emits human-visible Warn/Info entries when the sink becomes unhealthy or starts dropping writes.
3. **Documentation polish.** The existing size-based rotation knobs (`max_size_mb`, `max_backups`, `max_age_days`, `also_log_to_console`) get clearer inline comments and a new wiki page covering operator-facing logging behavior — including the new cloud-error threshold knob.

Time-based rotation is **out of scope**.

## Goals

- A deletion of `logs/tmi.log` while the server is running results in the file being recreated, with at most milliseconds of writes lost to the unlinked inode.
- Cleanup scripts (`scripts/clean.py logs|files|all`) stop any running server before deleting log files.
- `make start-server` continues to fail fast (not auto-stop) if a server is already running — preserves the existing footgun guard.
- Cloud sink health degradation surfaces as a single, human-readable Warn entry rather than silent error counter growth.
- Operators can find every logging config knob in one place (wiki page) and self-document via inline YAML comments.

## Non-goals

- Time-based log rotation (e.g., "rotate at midnight"). Lumberjack already handles size-based rotation; no requirement surfaced for daily rotation.
- A configurable compression toggle (currently hardcoded `Compress: true`).
- Restoring writes lost to the unlink-to-reopen window. This is fundamentally impossible.
- Hardening every script that might touch `logs/`. The server-side `fsnotify` watchdog covers any third-party deleter; only the well-known cleanup scripts get the explicit guard.
- A configurable cloud-sink poll interval. Hardcoded at 60 seconds.

## Architecture

### Components

| Component | Location | Purpose |
|---|---|---|
| `logFileWatchdog` | `internal/slogging/file_watchdog.go` (new) | fsnotify-based: detects unlink/rename of the active log file and calls `lumberjack.Logger.Rotate()` to reopen. |
| `cloudWatchdog` | `internal/slogging/cloud_watchdog.go` (new) | Periodic health-check + consecutive-failure threshold for the configured `CloudLogWriter`. |
| `Logger` | `internal/slogging/logger.go` (modified) | Owns both watchdogs; starts them in `NewLogger`, stops them in `Close`. |
| `Config` | `internal/slogging/logger.go` (modified) | Adds `CloudErrorThreshold int`. |
| `Logging` | `internal/config/config.go` (modified) | Adds `CloudErrorThreshold` field with YAML key `cloud_error_threshold` and env var `TMI_LOG_CLOUD_ERROR_THRESHOLD`. Comments expanded on every existing field. |
| `_clean_logs` | `scripts/manage-server.py` (modified) | Existing helper used by `cmd_start`. Adds a running-server check that refuses cleanup if a server is detected. |
| `clean_logs` | `scripts/clean.py` (modified) | Calls `manage-server.py stop` first, then deletes log files. |
| Wiki page | GitHub Wiki (out-of-tree) | New "Operator Guide: Logging & Log Rotation" page. |

### Component details

#### `logFileWatchdog`

```
type logFileWatchdog struct {
    watcher    *fsnotify.Watcher
    fileLog    *lumberjack.Logger    // for Rotate()
    activePath string                // absolute path of tmi.log
    slogger    *slog.Logger
    done       chan struct{}
    once       sync.Once             // for idempotent Stop
}
```

`newLogFileWatchdog(lj *lumberjack.Logger, slogger *slog.Logger) (*logFileWatchdog, error)`:
1. Compute `activePath = filepath.Clean(lj.Filename)`.
2. Compute `dir = filepath.Dir(activePath)`.
3. Create `fsnotify.NewWatcher()`. On error, return.
4. Call `watcher.Add(dir)`. On error, close watcher, return.
5. Spawn the event-loop goroutine.

Event-loop pseudocode:

```
for {
    select {
    case ev := <-w.watcher.Events:
        if filepath.Clean(ev.Name) != w.activePath {
            continue                                  // unrelated file
        }
        if ev.Op&(fsnotify.Remove|fsnotify.Rename) == 0 {
            continue                                  // Create/Write/Chmod — ignore
        }
        // Stat the active path. If it exists and is a fresh file (lumberjack
        // already rotated it), skip — no reopen needed.
        if _, err := os.Stat(w.activePath); err == nil {
            continue                                  // lumberjack-internal rotation
        }
        // The active file is gone. Reopen.
        if err := w.fileLog.Rotate(); err != nil {
            w.slogger.Warn("log file watchdog: reopen failed",
                "path", w.activePath, "event", ev.Op.String(), "error", err)
            continue
        }
        w.slogger.Warn("log file unlinked or replaced; reopening",
            "path", w.activePath, "event", ev.Op.String())

    case err := <-w.watcher.Errors:
        if err != nil {
            w.slogger.Warn("log file watchdog: watcher error", "error", err)
        }

    case <-w.done:
        return
    }
}
```

Stop:
```
func (w *logFileWatchdog) Stop() {
    w.once.Do(func() {
        close(w.done)
        _ = w.watcher.Close()
    })
}
```

**Why the "stat after Rename/Remove" check works.** Lumberjack rotates by renaming `tmi.log` to `tmi-<timestamp>.log` and creating a new `tmi.log` immediately. fsnotify on Linux (inotify) delivers a `Rename` event for the source path. By the time the watchdog reads the event, lumberjack has already created the new file. `os.Stat(activePath)` succeeds, the watchdog continues silently. If a hostile or buggy external script does `mv tmi.log /tmp/foo`, no replacement file is created, `os.Stat` returns ENOENT, and the watchdog reopens.

**Known false-negative:** if an external actor `mv`s the file *and* races a replacement `tmi.log` into place before the watchdog's `os.Stat` runs (millisecond window), the watchdog will treat the swap as a lumberjack-internal rotation and skip. The new (attacker-controlled) file becomes the active log destination. This is an accepted limitation — the threat model is "operator forgot the server was running," not adversarial swap. If we ever need to defend against it, the fix is to also compare inode numbers (FD inode vs path inode) which crosses platforms cleanly.

#### `cloudWatchdog`

```
type cloudWatchdog struct {
    cloudHandler *CloudLogHandler
    cloudWriter  CloudLogWriter
    slogger      *slog.Logger
    pollInterval time.Duration   // 60s, hardcoded
    errorThreshold int            // configurable, default 5
    done         chan struct{}
    once         sync.Once

    // state observed only from the watchdog goroutine
    lastHealthy           bool
    lastObservedErrorCount int64
    consecutiveFailures   int
    failureWarnEmitted    bool   // suppress duplicate Warns
}
```

Goroutine loop, ticking every `pollInterval`:

```
ticker := time.NewTicker(w.pollInterval)
defer ticker.Stop()
for {
    select {
    case <-ticker.C:
        // Health-check transition
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        healthy := w.cloudWriter.IsHealthy(ctx)
        cancel()
        if healthy != w.lastHealthy {
            if healthy {
                w.slogger.Info("cloud log sink healthy",
                    "provider", w.cloudWriter.Name())
            } else {
                w.slogger.Warn("cloud log sink unhealthy",
                    "provider", w.cloudWriter.Name())
            }
            w.lastHealthy = healthy
        }

        // Error-rate alarm
        currentErrors := w.cloudHandler.ErrorCount()
        delta := currentErrors - w.lastObservedErrorCount
        if delta > 0 {
            w.consecutiveFailures += int(delta)
            if w.consecutiveFailures >= w.errorThreshold && !w.failureWarnEmitted {
                w.slogger.Warn("cloud log sink failing writes",
                    "provider", w.cloudWriter.Name(),
                    "threshold", w.errorThreshold,
                    "recent_errors", w.consecutiveFailures)
                w.failureWarnEmitted = true
            }
        } else if w.consecutiveFailures > 0 {
            // Quiet poll — at least one full interval with no new errors.
            // Treat as recovery.
            if w.failureWarnEmitted {
                w.slogger.Info("cloud log sink writes recovered",
                    "provider", w.cloudWriter.Name())
            }
            w.consecutiveFailures = 0
            w.failureWarnEmitted = false
        }
        w.lastObservedErrorCount = currentErrors

    case <-w.done:
        return
    }
}
```

`newCloudWatchdog` is only called by `NewLogger` when `config.CloudWriter != nil`. Otherwise no goroutine is started.

If `errorThreshold <= 0`, the consecutive-failure alarm is disabled (the error-rate branch of the loop is skipped entirely). The health-check transition logic still runs, since it has independent value. To disable the entire watchdog including health checks, the operator can set the cloud writer to nil in config — i.e., disable cloud logging entirely.

`Stop()` mirrors `logFileWatchdog.Stop()` (idempotent close of `done`).

#### `Logger` integration

`NewLogger` extension:
1. After building `fileLogger` and the slogger, call `newLogFileWatchdog(fileLogger, slogger)`. On error, log Warn with the slogger and continue with `watchdog == nil`.
2. After the cloud handler is built (if any), call `newCloudWatchdog(cloudHandler, cloudWriter, slogger, errorThreshold)`. On error, log Warn and continue.
3. Store both watchdog references on the `Logger` struct.

`Logger.Close()` extension (order matters):
1. Stop the `cloudWatchdog` first (it depends on the cloud handler).
2. Stop the `logFileWatchdog`.
3. Close the cloud handler (existing).
4. Close the file logger (existing).

#### Script-side guards

**`scripts/manage-server.py`** — `_running_server_pid(project_root) -> Optional[int]`:
1. If `.server.pid` exists and parses to an integer PID, call `os.kill(pid, 0)`. If it succeeds, return that PID.
2. Otherwise run `ps aux`, scan for lines containing `bin/tmiserver` (excluding `grep` lines), return the first PID found.
3. Otherwise return `None`.

`_clean_logs` extension: at the very top, call `_running_server_pid`. If non-`None`, log a clear error and `sys.exit(1)`:
```
log_error(f"Cannot clean logs: TMI server is running (PID {pid}).")
log_error("Run 'make stop-server' first.")
sys.exit(1)
```

Net effect on `cmd_start`: if a server is running, the existing port-in-use check fires first (most cases), or the new running-server check fires (when the running server is on a different port). Either way, `cmd_start` fails fast without touching log files.

**`scripts/clean.py`** — `clean_logs()` extension:
1. Before deleting any files, run `manage-server.py stop` via `run_cmd`. The existing helper already handles "no server running" gracefully (returns 0 with a no-op message).
2. Then proceed with existing deletion logic.

`clean_files()` already calls `clean_logs()`, so it inherits the guard.
`clean_all()` already calls `clean_process()` first; the new `clean_logs()` behavior is redundant but harmless on this path.

### Configuration

New field on `internal/config/config.go` `Logging` struct:

```go
// CloudErrorThreshold is the number of consecutive cloud-sink write failures
// (observed at 60-second polling intervals) that must accumulate before a
// single Warn entry is emitted. Default 5. The counter resets on a quiet
// poll (no new errors during one interval).
CloudErrorThreshold int `yaml:"cloud_error_threshold" env:"TMI_LOG_CLOUD_ERROR_THRESHOLD"`
```

Default in `NewConfig` (or wherever the existing logging defaults live): `5`.

Plumbed through `cmd/server/main.go`:
```go
loggerConfig.CloudErrorThreshold = cfg.Logging.CloudErrorThreshold
```

Plumbed through `migratable_settings.go`:
```go
{Key: "logging.cloud_error_threshold", Value: strconv.Itoa(c.Logging.CloudErrorThreshold), Type: "int", ...}
```

Inline comment improvements on existing fields (in `internal/config/config.go`):

```go
type Logging struct {
    Level string  // ...

    // MaxAgeDays: days to retain rotated backup files before lumberjack
    // deletes them. 0 = retain indefinitely (subject to MaxBackups).
    // Default: 7.
    MaxAgeDays int `yaml:"max_age_days" env:"TMI_LOG_MAX_AGE_DAYS"`

    // MaxSizeMB: maximum size of the active log file (logs/tmi.log) in
    // megabytes before lumberjack rotates it. The active file is renamed
    // with a timestamp suffix and a fresh tmi.log is created. Default: 100.
    MaxSizeMB int `yaml:"max_size_mb" env:"TMI_LOG_MAX_SIZE_MB"`

    // MaxBackups: maximum number of rotated backup files to retain.
    // 0 = retain all (subject to MaxAgeDays). Default: 10.
    MaxBackups int `yaml:"max_backups" env:"TMI_LOG_MAX_BACKUPS"`

    // AlsoLogToConsole: if true, also write logs to stdout/stderr in
    // addition to the file. Useful for development. Default: true (dev),
    // false (prod).
    AlsoLogToConsole bool `yaml:"also_log_to_console" env:"TMI_LOG_ALSO_LOG_TO_CONSOLE"`

    // CloudErrorThreshold: number of consecutive cloud-sink write failures
    // (observed at 60-second poll intervals) before a single Warn entry is
    // emitted. The counter resets on a quiet interval. Set to 0 to disable
    // the alarm entirely. Default: 5.
    CloudErrorThreshold int `yaml:"cloud_error_threshold" env:"TMI_LOG_CLOUD_ERROR_THRESHOLD"`

    // ... existing fields
}
```

Matching comment improvements in `config-development.yml` and `config-development-oci.yml`.

### Wiki page

New page: **"Operator Guide: Logging & Log Rotation"**

Sections:
1. **What writes to the log file.** Brief description of `logs/tmi.log`, the lumberjack rotator, and the structured slog format.
2. **Configuration knobs.** Table of every `logging.*` YAML key, environment variable equivalent, default, and behavior. Includes the new `cloud_error_threshold`.
3. **Rotation behavior.** How size-based rotation works (rename + new file), the file name pattern of backups (`tmi-<timestamp>.log` or `tmi-<timestamp>.log.gz`), and retention semantics (`max_age_days` × `max_backups`).
4. **Cloud logging.** When the cloud sink is configured, what `cloud_error_threshold` controls, and what the Warn/Info messages mean.
5. **Recovery from accidental deletion.** Operational warning: do not delete `logs/tmi.log` while the server is running. The fsnotify watchdog will recover, but writes during the brief window between unlink and reopen are lost. The cleanup scripts (`make clean-logs`, etc.) auto-stop the server before cleaning to avoid this.

## Data flow

### Startup
1. `cmd/server/main.go` calls `slogging.Initialize(config)`.
2. `NewLogger` builds the `lumberjack.Logger`, the slog handler chain, and the cloud handler if configured.
3. `NewLogger` constructs `logFileWatchdog`. If construction fails, log Warn via the slogger and continue with `watchdog == nil`.
4. If a `CloudWriter` is configured, `NewLogger` constructs `cloudWatchdog`. If construction fails, log Warn and continue.
5. Returned `Logger` holds references to both watchdogs.

### Steady state
- Both watchdog goroutines block on `select` over their event channel and `done`. No log output. No CPU activity beyond kernel inotify notifications and a 60-second timer wakeup.

### External delete of `logs/tmi.log`
1. Some script (or user) runs `rm logs/tmi.log` while the server is running.
2. Kernel emits `Remove` for `logs/tmi.log`.
3. Watchdog's event loop confirms the path matches, stats the active path (ENOENT), calls `fileLogger.Rotate()`.
4. Lumberjack closes the stale FD and creates a fresh `logs/tmi.log`.
5. Watchdog emits one Warn: `log file unlinked or replaced; reopening  path=logs/tmi.log event=REMOVE`.
6. Subsequent log writes go to the fresh file.

### Lumberjack-internal rotation (size triggered)
1. Lumberjack renames `tmi.log` → `tmi-<timestamp>.log`, creates new `tmi.log`.
2. Kernel emits `Rename` for the old path.
3. Watchdog's event loop receives `Rename`, stats the active path → file exists (lumberjack already created it).
4. Watchdog skips silently. No Warn.

### Cloud sink degradation
1. `IsHealthy()` returns false on a 60-second poll.
2. Watchdog logs `Warn  cloud log sink unhealthy  provider=<name>`.
3. On a later poll, `IsHealthy()` returns true.
4. Watchdog logs `Info  cloud log sink healthy  provider=<name>`.

Independently:
1. `errorCount` grows by 5+ across one or more poll intervals (with no quiet interval in between).
2. Watchdog logs `Warn  cloud log sink failing writes  provider=<name> threshold=5 recent_errors=<N>`.
3. After a quiet interval, watchdog logs `Info  cloud log sink writes recovered`.

## Error handling

| Failure | Behavior |
|---|---|
| `fsnotify.NewWatcher()` returns error | Log Warn once. Server starts without file watchdog. |
| `watcher.Add(dir)` returns error | Log Warn once. Close partially-built watcher. Server starts without file watchdog. |
| `watcher.Errors` emits during steady state | Log Warn. Watchdog continues. |
| `lj.Rotate()` returns error inside event handler | Log Warn with the error. Watchdog continues. Subsequent writes via the existing FD continue to land on the unlinked inode until lumberjack's own size-triggered rotation. |
| `watcher.Events` channel closes | Treated as external shutdown. Goroutine exits cleanly. |
| `IsHealthy()` panics or hangs | The 5-second context timeout bounds it. A panic from a third-party writer would crash the goroutine; we wrap the call in a `defer recover()` that logs Warn and treats the call as "unhealthy". |
| `cloudHandler == nil` at watchdog construction | Watchdog goroutine is never started. |
| `Logger.Close()` called twice | Safe — `Stop()` uses `sync.Once`. |
| `clean.py logs` called with no running server | `manage-server.py stop` is a no-op; cleanup proceeds normally. |
| `cmd_start` called while server is running on a different port | New running-server check inside `_clean_logs` fires before any deletion. Logs error pointing to `make stop-server`, exits 1. |

## Testing

### Unit tests

**`internal/slogging/file_watchdog_test.go`** (new):
1. **Reopen on delete.** Start logger pointed at a tempdir, write a line, `os.Remove()` the active file, write another line, assert: (a) active file exists again, (b) the second line is in the new file, (c) one Warn entry produced.
2. **No reopen for unrelated files.** Create + remove `logs/unrelated.txt` in the same dir; assert no extra Warn entries, active log untouched.
3. **No spurious reopen for lumberjack rotation.** Directly call `lj.Rotate()`; assert no Warn from the watchdog.
4. **Idempotent stop.** Call `Stop()` twice; no panic. Verify no goroutine leak with `go.uber.org/goleak`.
5. **Watcher startup failure is non-fatal.** Inject a constructor that returns an error (via package-private factory variable that tests override); assert `NewLogger` succeeds and emits one Warn.

**`internal/slogging/cloud_watchdog_test.go`** (new):
1. **Health-check transition.** Use a fake `CloudLogWriter` whose `IsHealthy` flips between calls; assert one Warn on healthy→unhealthy, one Info on unhealthy→healthy, no log on stable state.
2. **Consecutive-failure threshold.** Use a fake writer whose `WriteLog` returns an error; trigger N=5 errors; assert one Warn. Trigger 5 more errors; assert no second Warn (suppressed).
3. **Recovery.** After errors, simulate one quiet poll (no new errors); assert one Info `cloud log sink writes recovered`. Trigger 5 more errors; assert a fresh Warn (counter reset).
4. **No watchdog when cloudWriter is nil.** Construct logger with `CloudWriter: nil`; verify no watchdog goroutine via goleak.
5. **Configurable threshold.** Set `CloudErrorThreshold=10`; assert no Warn at 5 errors, one Warn at 10.

### Script tests

**`scripts/test_manage_server_clean.py`** (new, pytest-style):
1. With a fake `.server.pid` containing `os.getpid()` and `logs/tmi.log` present, invoke `_clean_logs` → assert `SystemExit(1)` and `logs/tmi.log` untouched.
2. With no PID file and no running `bin/tmiserver` process, invoke `_clean_logs` → assert `logs/` is cleaned.

**`scripts/test_clean_logs_stops_server.py`** (new, pytest-style):
1. Create a fake "running server" via a sleeping subprocess with a PID file pointing at it; invoke `clean_logs()`; assert `manage-server.py stop` is invoked (mock) and that `logs/` is cleaned afterward.

If pytest-style tests don't fit existing infrastructure, the test plan downgrades these to manually-verified procedures documented in the implementation plan.

### Manual verification

1. Start the server (`make start-dev`). Confirm `logs/tmi.log` exists and is being written.
2. `rm logs/tmi.log`. Within milliseconds, `logs/tmi.log` reappears. The server log shows one Warn entry: `log file unlinked or replaced; reopening`.
3. Write enough output to trigger a lumberjack rotation (or set `max_size_mb: 1` for testing). Confirm rotation happens silently — no Warn from the watchdog.
4. With server running, `make clean-logs` → expect failure with clear error, server still running, log file intact.
5. With server running, `make clean-everything` → expect server stopped, then logs cleaned.

## Out of scope (explicit, with rationale)

- **Time-based rotation.** Dropped by user during brainstorming; lumberjack handles size-based; daily rotation is a feature ask, not a bug.
- **Compression toggle.** No requirement surfaced; `Compress: true` remains hardcoded.
- **Hardening every script that might delete logs.** The fsnotify watchdog covers third-party deleters; only the well-known cleanup scripts get explicit guards.
- **Lost writes during the unlink-to-reopen window.** Impossible to recover by definition.
- **Configurable cloud-watchdog poll interval.** Hardcoded 60s; if operator pressure surfaces, add as a follow-up.

## Files touched

| File | Change |
|---|---|
| `internal/slogging/file_watchdog.go` | New — fsnotify watchdog. |
| `internal/slogging/cloud_watchdog.go` | New — cloud sink health/error watchdog. |
| `internal/slogging/file_watchdog_test.go` | New. |
| `internal/slogging/cloud_watchdog_test.go` | New. |
| `internal/slogging/logger.go` | Modified — wire watchdogs into `NewLogger`/`Close`; add `Config.CloudErrorThreshold`. |
| `internal/config/config.go` | Modified — add `Logging.CloudErrorThreshold`; expand inline comments on existing fields. |
| `internal/config/migratable_settings.go` | Modified — register `logging.cloud_error_threshold`. |
| `cmd/server/main.go` | Modified — pass `CloudErrorThreshold` into the logger config. |
| `config-development.yml` | Modified — comments only; add `cloud_error_threshold` (commented-out default). |
| `config-development-oci.yml` | Modified — same. |
| `scripts/manage-server.py` | Modified — `_running_server_pid` helper; guard in `_clean_logs`. |
| `scripts/clean.py` | Modified — `clean_logs` calls `manage-server.py stop` first. |
| `scripts/test_manage_server_clean.py` | New (or manual procedure if pytest infra unavailable). |
| `scripts/test_clean_logs_stops_server.py` | New (or manual procedure if pytest infra unavailable). |
| `go.mod` | Modified — add direct dependency on `github.com/fsnotify/fsnotify`. |
| Wiki page | New — "Operator Guide: Logging & Log Rotation". |

## Implementation sequence

1. Add fsnotify direct dependency, build, confirm clean.
2. `logFileWatchdog` + tests.
3. `cloudWatchdog` + tests; add `CloudErrorThreshold` config plumbing.
4. Wire both into `NewLogger`/`Close`.
5. `manage-server.py` running-server guard + tests.
6. `clean.py` auto-stop change + tests.
7. Inline comments on `Logging` struct + YAML files.
8. Wiki page.
9. Manual verification per test plan.
10. Lint, build, test-unit, test-integration.
11. Oracle DB review — N/A (no DB-touching changes).
