"""Kubectl port-forward supervisor lifecycle: pidfiles, orphan detection, reaping.

Self-contained (no dependency on deploy.py or cluster.py) so any caller that
spawns a supervised `kubectl port-forward` loop (deploy.py's dev-up forwards,
and future callers such as #578) can reuse the same safety-checked reap logic
instead of re-inventing pattern matching against `ps`/`pgrep` output.

Background (#580): a supervised forward is spawned with start_new_session=True
so its pidfile can kill the whole process group on stop. But if the pidfile is
lost or goes stale (orchestrator crash, manual `rm`, a pre-existing forward from
before this module existed), the supervisor is reparented to init and nothing
can find it via the pidfile anymore — it keeps re-forwarding forever, silently
squatting on its port. reap_supervisors() finds and kills these via `pgrep`
pattern-matching, with deliberately conservative verification (see
reap_supervisors' docstring) so it can never touch an unrelated process.
"""
from __future__ import annotations

import json
import os
import signal
import subprocess
from dataclasses import dataclass
from pathlib import Path

# Verbatim token embedded in every new supervisor's command line (as a trailing
# `sh -c` comment — inert to the shell, but visible to `ps`/`pgrep -f`). Lets
# find_supervisors/reap_supervisors recognize supervisors spawned by this
# module without depending on any particular kubectl invocation shape.
PF_MARKER = "TMI_DEV_PORTFORWARD"


@dataclass
class ForwardRecord:
    pid: int
    context: str = ""
    port: int | None = None


def marker_for(pid_path: str | Path) -> str:
    """Return the marker token to embed for a supervisor tied to pid_path.

    Ties the marker to the pidfile's basename so reap calls scoped to one
    pidfile (e.g. _spawn_supervised_forward re-launching a single forward)
    don't also match a different forward's orphans.
    """
    return f"{PF_MARKER}={Path(pid_path).name}"


def read_pidfile(pid_path: str | Path) -> ForwardRecord | None:
    """Read a port-forward pidfile, returning None if missing/unparseable.

    Accepts the current one-line-JSON format ({"pid": N, "context": "...",
    "port": N}) and the legacy bare-integer format (just the PID, no context
    or port) written before this module existed.
    """
    p = Path(pid_path)
    try:
        text = p.read_text().strip()
    except (FileNotFoundError, OSError):
        return None
    if not text:
        return None
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return None
    if isinstance(data, int):
        # Legacy format: a bare integer PID, nothing else known. (A bare
        # integer is itself valid JSON, so this falls out of the same
        # json.loads() call rather than the except branch above.)
        return ForwardRecord(pid=data)
    if not isinstance(data, dict) or "pid" not in data:
        return None
    try:
        pid = int(data["pid"])
    except (TypeError, ValueError):
        return None
    return ForwardRecord(
        pid=pid,
        context=str(data.get("context", "") or ""),
        port=data.get("port"),
    )


def write_pidfile(pid_path: str | Path, pid: int, *, context: str = "", port: int | None = None) -> None:
    """Write a port-forward pidfile as one-line JSON."""
    payload = {"pid": pid, "context": context, "port": port}
    Path(pid_path).write_text(json.dumps(payload))


def port_listeners(port: int) -> list[tuple[int, str]]:
    """Return (pid, command) pairs for processes LISTENing on 127.0.0.1:port.

    Uses `lsof -nP -iTCP:{port} -sTCP:LISTEN -Fpc` (machine-parseable -F output:
    a 'p<pid>' line followed by a 'c<command>' line per match, with 'f<fd>'
    lines interspersed that this parser ignores) purely to discover which
    pids are LISTENing. lsof's `-F c` field is only the COMMAND NAME (e.g.
    "kubectl"), never the full argv — that made a real `kubectl port-forward`
    process indistinguishable from `kubectl get pods` to is_kubectl_forward(),
    which requires seeing "port-forward" in the command string. Each pid's
    FULL command line is resolved separately via _resolve_full_commands().

    Returns [] if nothing is listening or lsof is not on PATH.
    """
    try:
        result = subprocess.run(
            ["lsof", "-nP", f"-iTCP:{port}", "-sTCP:LISTEN", "-Fpc"],
            capture_output=True, text=True, check=False,
        )
    except FileNotFoundError:
        return []
    if result.returncode != 0:
        return []
    listeners: list[tuple[int, str]] = []
    seen_pids: set[int] = set()
    pid: int | None = None
    for line in result.stdout.splitlines():
        if not line:
            continue
        tag, value = line[0], line[1:]
        if tag == "p":
            try:
                pid = int(value)
            except ValueError:
                pid = None
        elif tag == "c" and pid is not None:
            if pid not in seen_pids:  # lsof emits one p/c pair per fd match
                seen_pids.add(pid)
                listeners.append((pid, value))
            pid = None
    if not listeners:
        return []
    return _resolve_full_commands(listeners)


def _resolve_full_commands(listeners: list[tuple[int, str]]) -> list[tuple[int, str]]:
    """Replace each lsof-reported command NAME with its full command LINE.

    Single batched `ps -o pid=,command= -p pid1,pid2,...` call for every pid
    in `listeners` — one subprocess spawn regardless of listener count, and
    it closes the TOCTOU window a separate per-pid lookup would have (matches
    the batching already used by _ps_pgid_and_command/_candidate_matches).

    Two distinct degrade-gracefully cases, handled differently:
      - `ps` itself is unavailable or the whole call errors (not on PATH,
        non-zero exit): fall back to the original lsof command NAMEs for
        every listener, rather than raising or silently returning [].
      - One specific pid is missing from an otherwise-successful ps listing
        (it exited in the gap between the lsof and ps calls): drop that pid
        from the result entirely rather than keeping a stale lsof name for
        it — a process that's already gone isn't "listening" anymore, so
        omitting it is the semantically correct answer, not a fallback.
    """
    pids = [pid for pid, _cmd in listeners]
    try:
        result = subprocess.run(
            ["ps", "-o", "pid=,command=", "-p", ",".join(str(p) for p in pids)],
            capture_output=True, text=True, check=False,
        )
    except FileNotFoundError:
        return listeners
    if result.returncode != 0:
        return listeners
    resolved: dict[int, str] = {}
    for line in result.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        parts = line.split(None, 1)
        if not parts or not parts[0].isdigit():
            continue
        resolved[int(parts[0])] = parts[1] if len(parts) > 1 else ""
    return [(pid, resolved[pid]) for pid, _cmd in listeners if pid in resolved]


def is_kubectl_forward(command: str) -> bool:
    """True if a process's command string looks like one of our kubectl
    port-forward supervisors or their kubectl child.

    Requires "port-forward" AND either "kubectl" (the actual forwarding
    process, or the supervisor's `while true; do kubectl ...` loop) or our
    PF_MARKER token (a supervisor whose kubectl argv doesn't literally contain
    "kubectl" as a substring some other way — belt and suspenders). This is
    intentionally narrow: an unrelated process would need to independently
    contain both "port-forward" and "kubectl" (or our invented marker token)
    in its command line, which no other process on a dev machine plausibly
    does.
    """
    return "port-forward" in command and ("kubectl" in command or PF_MARKER in command)


def _ps_pgid_and_command(pid: int) -> tuple[int | None, str]:
    """One atomic `ps -o pgid=,command= -p PID` snapshot of a candidate's
    process-group id and full command line. (pgid,) is None and command is ""
    if the pid is already gone by the time we look — a single combined call
    avoids the TOCTOU window a separate pgid-then-command lookup would have.
    """
    result = subprocess.run(
        ["ps", "-o", "pgid=,command=", "-p", str(pid)],
        capture_output=True, text=True, check=False,
    )
    line = result.stdout.strip("\n")
    if not line.strip():
        return None, ""
    parts = line.strip().split(None, 1)
    if not parts or not parts[0].isdigit():
        return None, ""
    pgid = int(parts[0])
    command = parts[1] if len(parts) > 1 else ""
    return pgid, command


def _candidate_matches(pid: int, pattern: str) -> tuple[bool, int | None, str]:
    """Verify a pgrep-nominated candidate pid against the full safety checklist.

    Returns (matches, pgid, command). pgrep only nominates; this is the actual
    gate. Requires ALL of: command contains `pattern`; command contains
    "while true" (the supervisor shell loop shape) OR is a kubectl forward
    itself (the "port-forward" + "kubectl"/marker check); pid is not us; pgid
    is not our own process group (never touch our own group).
    """
    pgid, command = _ps_pgid_and_command(pid)
    if pgid is None or not command or pattern not in command:
        return False, None, command
    shaped = "while true" in command and "port-forward" in command
    if not (shaped or is_kubectl_forward(command)):
        return False, None, command
    if pid == os.getpid():
        return False, None, command
    if pgid == os.getpgrp():
        return False, None, command
    return True, pgid, command


def find_supervisors(pattern: str) -> list[int]:
    """Return PIDs of live supervisor-shaped processes whose command line
    contains `pattern`, after full verification (see _candidate_matches).

    `pgrep -f pattern` only nominates candidates — never trust it alone. Each
    candidate is re-verified via `ps` before being included.
    """
    result = subprocess.run(
        ["pgrep", "-f", pattern],
        capture_output=True, text=True, check=False,
    )
    if result.returncode not in (0, 1):  # 1 == no matches, not an error
        return []
    matches = []
    for line in result.stdout.splitlines():
        line = line.strip()
        if not line.isdigit():
            continue
        pid = int(line)
        ok, _pgid, _cmd = _candidate_matches(pid, pattern)
        if ok:
            matches.append(pid)
    return matches


def _group_members(pgid: int) -> list[tuple[int, str]]:
    """Return (pid, command) for every live process in process group pgid."""
    result = subprocess.run(
        ["ps", "-eo", "pid=,pgid=,command="],
        capture_output=True, text=True, check=False,
    )
    members = []
    for line in result.stdout.splitlines():
        parts = line.strip().split(None, 2)
        if len(parts) < 2:
            continue
        try:
            mpid, mpgid = int(parts[0]), int(parts[1])
        except ValueError:
            continue
        if mpgid != pgid:
            continue
        mcmd = parts[2] if len(parts) > 2 else ""
        members.append((mpid, mcmd))
    return members


def reap_supervisors(pattern: str) -> list[int]:
    """SIGTERM every verified supervisor matching `pattern`; return reaped PIDs.

    NEVER uses raw `pkill -f` — pgrep only nominates, `_candidate_matches`
    (command contains pattern + "while true"/kubectl-forward shape, pid != us,
    pgid != our pgid) is the actual gate before anything is signaled.

    Kill policy, to avoid ever taking down an unrelated process sharing a
    group with a real target:
      - pid == pgid (the process is its own session/group leader, the normal
        shape for a freshly `start_new_session=True`-spawned supervisor):
        os.killpg(pgid) — safe, the group IS the supervisor + its kubectl child.
      - pid != pgid (a reparented orphan whose original session leader is
        gone, e.g. after the orchestrator that spawned it exited): enumerate
        every live member of that process group via `ps -eo pid=,pgid=,command=`.
        Only os.killpg(pgid) if EVERY member matches is_kubectl_forward() or
        the supervisor shape (contains pattern + "while true"). If even one
        group member doesn't match, the group is mixed/recycled (pgid reuse is
        possible on a long-running machine) and must never be group-killed —
        instead SIGTERM only the individually-verified matching pids.
    """
    reaped: list[int] = []
    seen_pgids: set[int] = set()
    result = subprocess.run(
        ["pgrep", "-f", pattern],
        capture_output=True, text=True, check=False,
    )
    if result.returncode not in (0, 1):
        return []
    candidates = [int(l) for l in result.stdout.splitlines() if l.strip().isdigit()]
    for pid in candidates:
        ok, pgid, _cmd = _candidate_matches(pid, pattern)
        if not ok:
            continue
        if pid == pgid:
            if pgid in seen_pgids:
                continue
            seen_pgids.add(pgid)
            try:
                os.killpg(pgid, signal.SIGTERM)
                reaped.append(pid)
            except (ProcessLookupError, PermissionError):
                pass
            continue
        # Reparented orphan: pid != pgid. Only group-kill if every live member
        # of the group matches; otherwise signal just this pid.
        if pgid in seen_pgids:
            # A prior candidate in this same group already triggered the
            # group-kill below (or tried to); nothing further to do for pid.
            if pid not in reaped:
                reaped.append(pid)
            continue
        members = _group_members(pgid)
        all_match = bool(members) and all(
            pattern in mcmd and ("while true" in mcmd or is_kubectl_forward(mcmd))
            for _mpid, mcmd in members
        )
        if all_match:
            seen_pgids.add(pgid)
            try:
                os.killpg(pgid, signal.SIGTERM)
                reaped.extend(mpid for mpid, _c in members)
            except (ProcessLookupError, PermissionError):
                pass
        else:
            try:
                os.kill(pid, signal.SIGTERM)
                reaped.append(pid)
            except (ProcessLookupError, PermissionError):
                pass
    return reaped
