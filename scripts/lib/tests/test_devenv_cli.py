"""CLI parser tests for scripts/devenv.py (the top-level orchestrator).

The module is loaded via importlib because it lives at scripts/devenv.py
(not inside scripts/lib/), so it is not on sys.path. The loader approach
keeps each test free of filesystem side-effects.
"""
import importlib.util
import sys
import unittest
from pathlib import Path

# ---------------------------------------------------------------------------
# Module loader — load scripts/devenv.py as "devenv_cli"
# ---------------------------------------------------------------------------
_DEVENV_PY = Path(__file__).resolve().parents[2] / "devenv.py"
_spec = importlib.util.spec_from_file_location("devenv_cli", _DEVENV_PY)
devenv_cli = importlib.util.module_from_spec(_spec)
# Make scripts/lib importable so devenv_cli's imports succeed
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
_spec.loader.exec_module(devenv_cli)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

class TestVerbsRegistered(unittest.TestCase):
    """All required verbs must appear in VERBS."""

    def test_all_verbs_registered(self):
        expected = {"up", "down", "restart", "reset", "nuke",
                    "status", "deploy", "logs", "cluster", "db"}
        self.assertTrue(expected <= set(devenv_cli.VERBS))


class TestParserDefaults(unittest.TestCase):
    """Parser defaults and option handling."""

    def test_parse_up_defaults_to_postgres(self):
        args = devenv_cli.build_parser().parse_args(["up"])
        self.assertEqual(args.verb, "up")
        self.assertEqual(args.db, "postgres")

    def test_parse_up_defaults_to_kind(self):
        args = devenv_cli.build_parser().parse_args(["up"])
        self.assertEqual(args.cluster, "kind")

    def test_parse_up_no_workers_default_false(self):
        args = devenv_cli.build_parser().parse_args(["up"])
        self.assertFalse(args.no_workers)

    def test_parse_up_yes_default_false(self):
        args = devenv_cli.build_parser().parse_args(["up"])
        self.assertFalse(args.yes)


class TestParserDbOption(unittest.TestCase):
    """--db option must work both before and after the verb."""

    def test_parse_up_oracle_after_verb(self):
        """'up --db oracle' — option after verb (used by unit tests)."""
        args = devenv_cli.build_parser().parse_args(["up", "--db", "oracle"])
        self.assertEqual(args.db, "oracle")

    def test_parse_db_oracle_before_verb(self):
        """'--db oracle up' — option before verb (used by Makefile wrappers)."""
        args = devenv_cli.build_parser().parse_args(["--db", "oracle", "up"])
        self.assertEqual(args.db, "oracle")

    def test_parse_no_workers_after_verb(self):
        args = devenv_cli.build_parser().parse_args(["up", "--no-workers"])
        self.assertTrue(args.no_workers)

    def test_no_workers_before_verb(self):
        args = devenv_cli.build_parser().parse_args(["--no-workers", "up"])
        self.assertTrue(args.no_workers)

    def test_parse_yes_after_verb(self):
        args = devenv_cli.build_parser().parse_args(["up", "--yes"])
        self.assertTrue(args.yes)


class TestParserClusterOption(unittest.TestCase):
    """--cluster option must work both before and after the verb, orthogonal to --db."""

    def test_parse_cluster_k3s_after_verb(self):
        """'up --cluster k3s' — option after verb (used by unit tests)."""
        args = devenv_cli.build_parser().parse_args(["up", "--cluster", "k3s"])
        self.assertEqual(args.cluster, "k3s")

    def test_parse_cluster_k3s_before_verb(self):
        """'--cluster k3s up' — option before verb (used by Makefile wrappers)."""
        args = devenv_cli.build_parser().parse_args(["--cluster", "k3s", "up"])
        self.assertEqual(args.cluster, "k3s")

    def test_parse_db_and_cluster_together(self):
        """--db and --cluster are orthogonal and coexist in either order."""
        args = devenv_cli.build_parser().parse_args(["--db", "oracle", "--cluster", "k3s", "nuke"])
        self.assertEqual(args.db, "oracle")
        self.assertEqual(args.cluster, "k3s")

    def test_cluster_accepts_docker_desktop(self):
        args = devenv_cli.build_parser().parse_args(["--cluster", "docker-desktop", "up"])
        self.assertEqual(args.cluster, "docker-desktop")


class TestClusterAndDbSubcmds(unittest.TestCase):
    """'cluster' and 'db' take a positional action."""

    def test_cluster_up(self):
        args = devenv_cli.build_parser().parse_args(["cluster", "up"])
        self.assertEqual(args.verb, "cluster")
        self.assertEqual(args.action, "up")

    def test_cluster_down(self):
        args = devenv_cli.build_parser().parse_args(["cluster", "down"])
        self.assertEqual(args.action, "down")

    def test_db_up(self):
        args = devenv_cli.build_parser().parse_args(["db", "up"])
        self.assertEqual(args.verb, "db")
        self.assertEqual(args.action, "up")

    def test_db_down(self):
        args = devenv_cli.build_parser().parse_args(["db", "down"])
        self.assertEqual(args.action, "down")


class TestDispatchTableComplete(unittest.TestCase):
    """Every verb in VERBS must have a dispatch entry."""

    def test_dispatch_covers_all_verbs(self):
        for verb in devenv_cli.VERBS:
            self.assertIn(verb, devenv_cli._DISPATCH,
                          f"Missing dispatch for verb: {verb}")


if __name__ == "__main__":
    unittest.main()
