"""Tests for scripts/container_build_helpers.py's scan gate.

Loaded via importlib because the module lives at scripts/ (not scripts/lib/),
the same approach as test_clean.py.
"""

import importlib.util
import json
import subprocess
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

_HELPERS_PY = Path(__file__).resolve().parents[2] / "container_build_helpers.py"
_spec = importlib.util.spec_from_file_location("container_build_helpers", _HELPERS_PY)
helpers = importlib.util.module_from_spec(_spec)
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
_spec.loader.exec_module(helpers)


class ScanImageSourceTestCase(unittest.TestCase):
    """scan_image must scan the pushed image, not a stale daemon copy."""

    def _run_scan(self, **kwargs):
        seen: list[list[str]] = []

        def fake_run(cmd, *, check=True, capture=False, cwd=None):
            seen.append(cmd)
            if cmd[0] == "grype":
                json_path = next(a for a in cmd if a.startswith("json=")).removeprefix("json=")
                Path(json_path).write_text(json.dumps({"source": {"type": "image"}, "matches": []}))
            return subprocess.CompletedProcess(cmd, 0, stdout="", stderr="")

        with TemporaryDirectory() as tmp, \
                mock.patch.object(helpers, "run", fake_run), \
                mock.patch.object(helpers, "ensure_grype_db_current"), \
                mock.patch.object(helpers, "_which", return_value=None):
            passed = helpers.scan_image("reg.example/tmi-redis:latest", Path(tmp), **kwargs)
        return passed, [c for c in seen if c[0] == "grype"]

    def test_default_scans_by_bare_name(self):
        passed, grype_cmds = self._run_scan()
        self.assertTrue(passed)
        self.assertEqual(grype_cmds[0][1], "reg.example/tmi-redis:latest")

    def test_from_registry_forces_registry_source(self):
        passed, grype_cmds = self._run_scan(from_registry=True)
        self.assertTrue(passed)
        self.assertEqual(grype_cmds[0][1], "registry:reg.example/tmi-redis:latest")


if __name__ == "__main__":
    unittest.main()
