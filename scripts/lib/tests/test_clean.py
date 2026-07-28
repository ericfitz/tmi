"""Tests for scripts/clean.py, focused on what it must NOT delete.

The module is loaded via importlib because it lives at scripts/clean.py (not
inside scripts/lib/), so it is not on sys.path — same approach as
test_devenv_cli.py.

These tests exist because `clean-files` twice grew the ability to destroy CATS
campaign corpora:

  * It cleaned `test/outputs/cats` with a preserve-list naming the
    pre-migration single database (`cats-results.db`). After the cats plugin
    moved results to `test/results/cats/` with per-run
    `cats-results-<run_id>.db` files, "fixing" that path would have made the
    preserve-list match nothing and deleted every run.
  * It ran `pkill -f cats`, a bare substring match that hits the plugin's own
    path, any unrelated process whose command line contains "cats", and
    potentially the invoking shell.

The plugin owns `test/results/cats/`: it prunes via `keep_runs` and explicitly
protects whatever `latest.db` points at. A second retention policy in tmi races
that one, and each campaign costs ~40 minutes to reproduce.
"""
import importlib.util
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

# ---------------------------------------------------------------------------
# Module loader — load scripts/clean.py as "clean_cli"
# ---------------------------------------------------------------------------
_CLEAN_PY = Path(__file__).resolve().parents[2] / "clean.py"
_spec = importlib.util.spec_from_file_location("clean_cli", _CLEAN_PY)
clean_cli = importlib.util.module_from_spec(_spec)
# Make scripts/lib importable so clean_cli's imports succeed
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
_spec.loader.exec_module(clean_cli)


class CleanFilesTestCase(unittest.TestCase):
    """Builds a fake project root and runs clean_files() against it."""

    def setUp(self):
        self._tmp = TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)

        # A CATS results directory shaped the way the plugin writes it.
        self.cats = self.root / "test" / "results" / "cats"
        self.cats.mkdir(parents=True)
        self.run_db = self.cats / "cats-results-20260728T062255Z.db"
        self.run_db.write_text("campaign corpus")
        (self.cats / "cats-results-20260728T062255Z.db-wal").write_text("")
        self.latest = self.cats / "latest.db"
        self.latest.symlink_to(self.run_db.name)
        self.ref_data = self.cats / "cats-test-data.yml"
        self.ref_data.write_text("refdata")

        # Things clean_files IS supposed to remove.
        (self.root / "logs").mkdir()
        self.stale_log = self.root / "logs" / "tmi.log"
        self.stale_log.write_text("noise")
        (self.root / "wstest").mkdir()
        self.wstest_log = self.root / "wstest" / "session.log"
        self.wstest_log.write_text("noise")

        # Record every subprocess clean_files would launch.
        self.commands = []
        self._orig_root = clean_cli.get_project_root
        self._orig_run_cmd = clean_cli.run_cmd
        clean_cli.get_project_root = lambda: self.root
        clean_cli.run_cmd = lambda cmd, **kwargs: self.commands.append(cmd)
        self.addCleanup(setattr, clean_cli, "get_project_root", self._orig_root)
        self.addCleanup(setattr, clean_cli, "run_cmd", self._orig_run_cmd)

    def test_cats_results_directory_is_untouched(self):
        """Every artifact the plugin owns must survive clean-files."""
        clean_cli.clean_files()

        self.assertTrue(self.run_db.exists(), "per-run campaign database was deleted")
        self.assertTrue(
            self.latest.is_symlink(), "latest.db symlink was deleted"
        )
        self.assertEqual(
            self.run_db.read_text(),
            "campaign corpus",
            "per-run campaign database was truncated",
        )
        self.assertTrue(self.ref_data.exists(), "refData file was deleted")

    def test_no_processes_are_killed(self):
        """clean-files must not kill processes; that is clean-process's job."""
        clean_cli.clean_files()

        for cmd in self.commands:
            self.assertNotIn(
                "pkill",
                cmd[0],
                f"clean_files launched a process-killing command: {cmd}",
            )

    def test_logs_are_still_removed(self):
        """The narrowing must not stop clean_files doing its actual job."""
        clean_cli.clean_files()

        self.assertFalse(self.stale_log.exists(), "log file was not cleaned")
        self.assertFalse(self.wstest_log.exists(), "wstest log was not cleaned")


if __name__ == "__main__":
    unittest.main()
