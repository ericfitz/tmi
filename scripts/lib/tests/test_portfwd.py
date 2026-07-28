"""Unit tests for scripts/lib/portfwd.py (#580).

Process-spawning tests use a UNIQUE per-test marker (a random suffix, never
the real pidfile-derived markers or the legacy "-n tmi-platform port-forward
svc/" pattern deploy.py uses) so these tests can never find, and therefore
can never touch, a real dev-environment port-forward supervisor running on
the same machine.
"""
import os
import signal
import socket
import subprocess
import sys
import time
import unittest
import uuid
from pathlib import Path
from tempfile import TemporaryDirectory

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import portfwd  # noqa: E402


def _wait_gone(pid: int, timeout: float = 5.0) -> bool:
    """Poll os.kill(pid, 0) until the process is gone (or timeout).

    Only valid for a pid that is NOT one of our own Popen children — a killed
    child of ours stays visible to kill(pid, 0) as a zombie until we wait()
    it, which would make this loop time out even though the signal worked.
    Use Popen.poll() (see _wait_gone_popen) for our own children instead.
    """
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return True
        time.sleep(0.05)
    return False


def _wait_gone_popen(proc: subprocess.Popen, timeout: float = 5.0) -> bool:
    """Poll Popen.poll() until our own child process has exited (and is reaped)."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if proc.poll() is not None:
            return True
        time.sleep(0.05)
    return False


class TestMarkerFor(unittest.TestCase):
    def test_marker_includes_pidfile_basename(self):
        self.assertEqual(
            portfwd.marker_for("/tmp/tmi-dev-portforward.pid"),
            "TMI_DEV_PORTFORWARD=tmi-dev-portforward.pid",
        )

    def test_marker_uses_basename_only_ignoring_directory(self):
        a = portfwd.marker_for("/some/dir/tmi-dev-redis-portforward.pid")
        b = portfwd.marker_for("tmi-dev-redis-portforward.pid")
        self.assertEqual(a, b)

    def test_different_pidfiles_get_different_markers(self):
        self.assertNotEqual(
            portfwd.marker_for("/tmp/tmi-dev-portforward.pid"),
            portfwd.marker_for("/tmp/tmi-dev-redis-portforward.pid"),
        )


class TestReadPidfile(unittest.TestCase):
    def test_reads_json_format(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            p.write_text('{"pid": 4242, "context": "k3s-rp", "port": 8080}')
            rec = portfwd.read_pidfile(p)
            self.assertEqual(rec, portfwd.ForwardRecord(pid=4242, context="k3s-rp", port=8080))

    def test_reads_legacy_bare_int(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            p.write_text("4242")
            rec = portfwd.read_pidfile(p)
            self.assertEqual(rec.pid, 4242)
            self.assertEqual(rec.context, "")
            self.assertIsNone(rec.port)

    def test_legacy_bare_int_with_whitespace(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            p.write_text("  4242 \n")
            rec = portfwd.read_pidfile(p)
            self.assertEqual(rec.pid, 4242)

    def test_garbage_returns_none(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            p.write_text("not json or a number")
            self.assertIsNone(portfwd.read_pidfile(p))

    def test_missing_file_returns_none(self):
        with TemporaryDirectory() as d:
            self.assertIsNone(portfwd.read_pidfile(Path(d) / "nope.pid"))

    def test_empty_file_returns_none(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            p.write_text("")
            self.assertIsNone(portfwd.read_pidfile(p))

    def test_json_without_pid_key_returns_none(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            p.write_text('{"context": "k3s-rp"}')
            self.assertIsNone(portfwd.read_pidfile(p))


class TestWritePidfile(unittest.TestCase):
    def test_round_trip(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            portfwd.write_pidfile(p, 999, context="k3s-rp", port=6379)
            rec = portfwd.read_pidfile(p)
            self.assertEqual(rec, portfwd.ForwardRecord(pid=999, context="k3s-rp", port=6379))

    def test_round_trip_defaults(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            portfwd.write_pidfile(p, 111)
            rec = portfwd.read_pidfile(p)
            self.assertEqual(rec.pid, 111)
            self.assertEqual(rec.context, "")
            self.assertIsNone(rec.port)

    def test_writes_single_line_json(self):
        with TemporaryDirectory() as d:
            p = Path(d) / "pf.pid"
            portfwd.write_pidfile(p, 111, context="docker-desktop", port=8080)
            text = p.read_text()
            self.assertEqual(len(text.splitlines()), 1)


class TestIsKubectlForward(unittest.TestCase):
    def test_positive_kubectl_forward(self):
        self.assertTrue(portfwd.is_kubectl_forward(
            "kubectl -n tmi-platform port-forward svc/tmi-server 8080:8080"))

    def test_positive_supervisor_loop(self):
        self.assertTrue(portfwd.is_kubectl_forward(
            "sh -c while true; do kubectl -n tmi-platform port-forward svc/redis 6379:6379"
            ">/dev/null 2>&1; sleep 1; done"))

    def test_positive_marker_without_literal_kubectl_word(self):
        # Defensive: the marker alone is also sufficient, in case a future
        # invocation shape doesn't spell "kubectl" as a literal substring.
        self.assertTrue(portfwd.is_kubectl_forward(
            f"port-forward svc/tmi-server {portfwd.PF_MARKER}=x.pid"))

    def test_negative_grep_for_marker_string(self):
        # Regression guard from the design: searching FOR the marker must not
        # itself look like a forward.
        self.assertFalse(portfwd.is_kubectl_forward("rg TMI_DEV_PORTFORWARD"))

    def test_negative_unrelated_http_server_on_same_port(self):
        self.assertFalse(portfwd.is_kubectl_forward("python -m http.server 8080"))

    def test_negative_kubectl_without_port_forward(self):
        self.assertFalse(portfwd.is_kubectl_forward("kubectl get pods -n tmi-platform"))

    def test_negative_empty_command(self):
        self.assertFalse(portfwd.is_kubectl_forward(""))


class TestPortListeners(unittest.TestCase):
    def test_own_listening_socket_is_reported(self):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.bind(("127.0.0.1", 0))
            s.listen(1)
            port = s.getsockname()[1]
            listeners = portfwd.port_listeners(port)
            pids = [pid for pid, _cmd in listeners]
            self.assertIn(os.getpid(), pids)

    def test_unused_port_returns_empty(self):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        # Socket closed immediately; nothing listens here now.
        self.assertEqual(portfwd.port_listeners(port), [])

    def test_reported_command_is_full_command_line_not_bare_name(self):
        # Regression guard: a prior implementation used lsof's `-F c` field
        # directly, which is only the COMMAND NAME (e.g. "python3"), not the
        # full argv. That made every real `kubectl port-forward` process
        # indistinguishable from `kubectl get pods` to is_kubectl_forward(),
        # which requires seeing "port-forward" in the command string. Spawn a
        # child whose argv carries a unique marker word that could never
        # appear in lsof's bare short command name ("python3"/"Python") — a
        # future refactor back to the lsof-only name must fail this test.
        marker = f"TMI_PORTFWD_TEST_MARKER_{uuid.uuid4().hex}"
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        script = (
            f"import socket,time\n"
            f"s=socket.socket(socket.AF_INET, socket.SOCK_STREAM)\n"
            f"s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)\n"
            f"s.bind(('127.0.0.1', {port}))\n"
            f"s.listen(1)\n"
            f"time.sleep(10)\n"
        )
        proc = subprocess.Popen([sys.executable, "-c", script, "--", marker])
        try:
            deadline = time.time() + 5.0
            listeners: list[tuple[int, str]] = []
            while time.time() < deadline and not listeners:
                listeners = portfwd.port_listeners(port)
                if not listeners:
                    time.sleep(0.05)
            mine = [cmd for pid, cmd in listeners if pid == proc.pid]
            self.assertEqual(len(mine), 1, f"expected exactly one entry for the child pid, got {listeners!r}")
            command = mine[0]
            self.assertIn("python", command.lower())
            # Proves this is the full argv, not the bare lsof command name:
            # the marker only appears on the full command line.
            self.assertIn(marker, command)
        finally:
            proc.kill()
            proc.wait(timeout=5)

    def test_kubectl_forward_listener_satisfies_is_kubectl_forward(self):
        # End-to-end regression for the actual #580 reclaim path: spawn a
        # process whose full argv (not its short command name) is what makes
        # it recognizable as a kubectl port-forward, bind the port from
        # inside it, and confirm the (pid, command) pair port_listeners()
        # returns for that pid passes is_kubectl_forward(). This is the exact
        # composition (_preflight_port calls both in sequence) that regressed:
        # port_listeners() previously returned only "kubectl", which flunks
        # is_kubectl_forward() even though the process genuinely is one.
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        # `exec -a kubectl` renames argv[0] to "kubectl" (so the lsof-reported
        # short command name would be "kubectl") while python actually binds
        # the port and holds it open, standing in for a real
        # `kubectl ... port-forward ...` child without needing a live cluster.
        script = (
            f"import socket,time,sys\n"
            f"s=socket.socket(socket.AF_INET, socket.SOCK_STREAM)\n"
            f"s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)\n"
            f"s.bind(('127.0.0.1', {port}))\n"
            f"s.listen(1)\n"
            f"time.sleep(10)\n"
        )
        proc = subprocess.Popen(
            ["sh", "-c", f'exec -a kubectl "$0" -c "$1" -- port-forward svc/fake {port}:{port}',
             sys.executable, script],
        )
        try:
            deadline = time.time() + 5.0
            listeners: list[tuple[int, str]] = []
            while time.time() < deadline and not listeners:
                listeners = portfwd.port_listeners(port)
                if not listeners:
                    time.sleep(0.05)
            matches = [(pid, cmd) for pid, cmd in listeners if "port-forward" in cmd]
            self.assertTrue(matches, f"no port-forward-shaped listener found; got {listeners!r}")
            for pid, command in matches:
                self.assertTrue(
                    portfwd.is_kubectl_forward(command),
                    f"pid {pid} command {command!r} should satisfy is_kubectl_forward",
                )
        finally:
            proc.kill()
            proc.wait(timeout=5)


class TestFindAndReapSupervisors(unittest.TestCase):
    """Orphan simulation — no cluster needed.

    Spawns two processes shaped like a real orphaned supervisor:
      (a) a direct `start_new_session=True` spawn, where pgid == pid (the
          shape of a supervisor whose pidfile is intact but was simply lost);
      (b) a "double-forked" orphan, backgrounded (`&`) from a session-leader
          bootstrap process that then exits immediately. Since job control is
          off under `sh -c`, the backgrounded child inherits the bootstrap's
          pgid rather than getting its own — so once the bootstrap exits, the
          child is reparented (ppid -> the machine's reaper) while its pgid
          keeps pointing at a pid that no longer exists. This mirrors the
          real #580 orphans (pgid 32419, no live process 32419).

    A marker-less decoy with the same "while true; do sleep 1; done" shape is
    also spawned to prove the marker-scoped pattern never over-matches.
    """

    def setUp(self):
        self._cleanup_pids: list[int] = []

    def tearDown(self):
        # Best-effort: SIGKILL anything a failed assertion left running.
        for pid in self._cleanup_pids:
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass

    def test_finds_and_reaps_orphans_without_touching_decoy(self):
        marker = f"TMI_DEV_PORTFORWARD=test-580-{uuid.uuid4().hex}.pid"
        shaped_cmd = f"while true; do sleep 1; done # port-forward svc/fake -n tmi-platform {marker}"

        # (a) direct supervisor: start_new_session=True => pid == pgid.
        direct = subprocess.Popen(["sh", "-c", shaped_cmd], start_new_session=True)
        self._cleanup_pids.append(direct.pid)

        # (b) double-forked orphan: pgid != pid (see class docstring).
        # shaped_cmd ends in a `# marker` comment, which would swallow the
        # rest of the line (the subshell's closing paren and `& echo $!`) if
        # they followed on the same line — so the closing paren goes on its
        # own line instead.
        bootstrap = subprocess.Popen(
            ["sh", "-c", f"({shaped_cmd}\n) & echo $!"],
            start_new_session=True, stdout=subprocess.PIPE, text=True,
        )
        try:
            child_pid_line = bootstrap.stdout.readline().strip()
        finally:
            bootstrap.stdout.close()
        bootstrap.wait(timeout=5)
        self.assertTrue(child_pid_line.isdigit(), f"expected a PID, got {child_pid_line!r}")
        orphan_pid = int(child_pid_line)
        self._cleanup_pids.append(orphan_pid)
        # Let the orphan actually be reparented / settle before we look for it.
        time.sleep(0.2)

        # Decoy: same shape, no marker — must never be matched or touched.
        decoy = subprocess.Popen(["sh", "-c", "while true; do sleep 1; done"], start_new_session=True)
        self._cleanup_pids.append(decoy.pid)
        time.sleep(0.2)

        try:
            found = set(portfwd.find_supervisors(marker))
            self.assertIn(direct.pid, found)
            self.assertIn(orphan_pid, found)
            self.assertNotIn(decoy.pid, found)

            reaped = set(portfwd.reap_supervisors(marker))
            self.assertIn(direct.pid, reaped)
            self.assertIn(orphan_pid, reaped)
            self.assertNotIn(decoy.pid, reaped)

            self.assertTrue(_wait_gone_popen(direct), "direct supervisor was not reaped")
            self.assertTrue(_wait_gone(orphan_pid), "double-forked orphan was not reaped")

            # The marker-less decoy must still be alive at this point.
            try:
                os.kill(decoy.pid, 0)
            except ProcessLookupError:
                self.fail("decoy (marker-less) process was killed by reap_supervisors")
        finally:
            decoy.kill()
            decoy.wait(timeout=5)
            direct.wait(timeout=5)

    def test_unrelated_pattern_finds_nothing(self):
        # No process on this machine should ever match a marker nobody used.
        marker = f"TMI_DEV_PORTFORWARD=test-580-unused-{uuid.uuid4().hex}.pid"
        self.assertEqual(portfwd.find_supervisors(marker), [])
        self.assertEqual(portfwd.reap_supervisors(marker), [])

    def test_pattern_starting_with_dash_is_not_parsed_as_a_pgrep_option(self):
        """A leading-dash pattern must still match.

        stop_port_forward()'s legacy tier reaps with "-n tmi-platform
        port-forward svc/". Without a `--` separator, pgrep parses the leading
        "-n" as its own option, exits with "illegal option", and finds nothing
        — so the reaper silently did nothing while reporting success. Observed
        against three real orphans that plainly matched the pattern.
        """
        token = f"dashopt-{uuid.uuid4().hex[:12]}"
        # Shape must satisfy _candidate_matches: while-true + port-forward.
        script = (
            f"while true; do sleep 1; done "
            f"# -n tmi-platform port-forward svc/{token}"
        )
        proc = subprocess.Popen(["sh", "-c", script], start_new_session=True)
        self._cleanup_pids.append(proc.pid)
        time.sleep(0.2)  # let it settle, matching the sibling orphan test

        pattern = f"-n tmi-platform port-forward svc/{token}"
        found = portfwd.find_supervisors(pattern)
        self.assertIn(proc.pid, found,
                      "leading-dash pattern must not be swallowed by pgrep")

        # reap_supervisors has its own pgrep call site — the original bug was
        # fixed in one and not the other, so assert both paths.
        reaped = portfwd.reap_supervisors(pattern)
        self.assertIn(proc.pid, reaped,
                      "reap_supervisors must honor a leading-dash pattern too")


if __name__ == "__main__":
    unittest.main()
