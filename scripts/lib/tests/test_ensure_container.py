import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import tmi_common


def _bind(src: str, dst: str) -> dict:
    return {"Type": "bind", "Source": src, "Destination": dst}


def _named(name: str, dst: str) -> dict:
    return {"Type": "volume", "Name": name, "Source": f"/var/lib/docker/volumes/{name}/_data", "Destination": dst}


class EnsureContainerTest(unittest.TestCase):
    """ensure_container reuses a container only when its mounts match the request."""

    def setUp(self):
        self.calls: list[list[str]] = []
        self.running = False
        self.exists = False
        self.mounts: list[dict] = []
        for target, side in (
            ("container_is_running", lambda n: self.running),
            ("container_exists", lambda n: self.exists),
            ("run_cmd", self._fake_run),
        ):
            p = mock.patch.object(tmi_common, target, side_effect=side)
            p.start()
            self.addCleanup(p.stop)
        self.tls = tempfile.mkdtemp()
        self.addCleanup(lambda: Path(self.tls).rmdir() if Path(self.tls).exists() else None)

    def _fake_run(self, cmd, **kwargs):
        self.calls.append(list(cmd))
        out = json.dumps(self.mounts) if cmd[:2] == ["docker", "inspect"] else ""
        return subprocess.CompletedProcess(cmd, 0, stdout=out, stderr="")

    def _verbs(self) -> list[str]:
        return [c[1] for c in self.calls]

    def _ensure(self, volumes):
        tmi_common.ensure_container("c1", 6380, 6379, "img", volumes=volumes)

    def test_running_matching_bind_is_noop(self):
        self.running = self.exists = True
        self.mounts = [_bind(self.tls, "/tls")]
        self._ensure({self.tls: "/tls"})
        self.assertNotIn("run", self._verbs())
        self.assertNotIn("rm", self._verbs())
        self.assertNotIn("start", self._verbs())

    def test_running_mismatched_bind_is_recreated(self):
        self.running = self.exists = True
        self.mounts = [_bind("/nonexistent/other-checkout/tls", "/tls")]
        self._ensure({self.tls: "/tls"})
        self.assertIn(["docker", "rm", "-f", "c1"], self.calls)
        run = [c for c in self.calls if c[:2] == ["docker", "run"]]
        self.assertEqual(len(run), 1)
        self.assertIn(f"{self.tls}:/tls", run[0])
        self.assertLess(self.calls.index(["docker", "rm", "-f", "c1"]), self.calls.index(run[0]))

    def test_stopped_matching_is_started_only(self):
        self.exists = True
        self.mounts = [_bind(self.tls, "/tls")]
        self._ensure({self.tls: "/tls"})
        self.assertIn(["docker", "start", "c1"], self.calls)
        self.assertNotIn("run", self._verbs())
        self.assertNotIn("rm", self._verbs())

    def test_stopped_mismatched_is_recreated(self):
        self.exists = True
        self.mounts = [_bind("/nonexistent/other-checkout/tls", "/tls")]
        self._ensure({self.tls: "/tls"})
        self.assertIn(["docker", "rm", "-f", "c1"], self.calls)
        self.assertIn("run", self._verbs())
        self.assertNotIn("start", self._verbs())

    def test_missing_is_created(self):
        self._ensure({self.tls: "/tls"})
        self.assertIn("run", self._verbs())
        self.assertNotIn("rm", self._verbs())

    def test_named_volume_matching_is_reused(self):
        self.running = self.exists = True
        self.mounts = [_named("pgdata", "/var/lib/postgresql/data")]
        self._ensure({"pgdata": "/var/lib/postgresql/data"})
        self.assertNotIn("run", self._verbs())
        self.assertNotIn("rm", self._verbs())

    def test_volumes_none_skips_inspect(self):
        self.running = self.exists = True
        self._ensure(None)
        self.assertNotIn("inspect", self._verbs())
        self.assertEqual(self.calls, [])

    def test_missing_mount_destination_is_recreated(self):
        self.running = self.exists = True
        self.mounts = []
        self._ensure({self.tls: "/tls"})
        self.assertIn(["docker", "rm", "-f", "c1"], self.calls)
        self.assertIn("run", self._verbs())


if __name__ == "__main__":
    unittest.main()
