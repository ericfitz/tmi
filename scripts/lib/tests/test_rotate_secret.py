import importlib.util
import unittest
from pathlib import Path
from unittest import mock

_p = Path(__file__).resolve().parents[2] / "rotate-secret.py"
_spec = importlib.util.spec_from_file_location("rotate_secret", _p)
rs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(rs)


class TestResolveContext(unittest.TestCase):
    def test_docker_desktop_ignores_ambient(self):
        self.assertEqual(rs.resolve_context("docker-desktop", None), "docker-desktop")

    def test_aws_requires_explicit_context(self):
        with self.assertRaises(ValueError):
            rs.resolve_context("aws", None)
        self.assertEqual(rs.resolve_context("aws", "my-eks"), "my-eks")

    def test_k3s_uses_configured_context(self):
        with mock.patch.object(rs.cluster, "expected_context", return_value="c1"):
            self.assertEqual(rs.resolve_context("k3s", None), "c1")

    def test_active_jobs(self):
        jobs = {"items": [
            {"metadata": {"name": "tmi-rotator-1"}, "status": {"active": 1}},
            {"metadata": {"name": "tmi-rotator-2"}, "status": {"succeeded": 1}},
            {"metadata": {"name": "other"}, "status": {"active": 1}}]}
        self.assertEqual(rs.active_rotator_jobs(jobs), ["tmi-rotator-1"])
