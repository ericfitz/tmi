"""Unit tests for scripts/run-integration-tests.py's pass/fail decision (#601).

The harness could skip the workflows package — the end-to-end API suite — and
still exit 0, so a run that verified nothing about the API contract reported
as a clean pass. These tests pin the rule that a skip is a failure.

Nothing here runs go test, Docker, or the network: `run_pg` is replaced with a
stub returning the (exit_code, workflows_skipped) pair the real one produces,
and the log parser is fed an empty temp file.
"""
import importlib.util
import sys
import unittest
from pathlib import Path
from unittest import mock

_SCRIPT = Path(__file__).resolve().parents[2] / "run-integration-tests.py"


def _load():
    """Import the hyphenated script by path (it is not an importable module)."""
    sys.path.insert(0, str(_SCRIPT.parent / "lib"))
    spec = importlib.util.spec_from_file_location("run_integration_tests", _SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


rit = _load()


class TestWorkflowSkipIsAFailure(unittest.TestCase):
    def _main_with(self, exit_code, skipped, stats=None):
        stats = stats or {"passed": 22, "failed": 0, "skipped": 1, "pkg_pass": 4, "pkg_fail": 0}
        args = mock.Mock(target="pg", verbose=False, quiet=False)
        with mock.patch.object(rit, "parse_args", return_value=args), \
                mock.patch.object(rit, "apply_verbosity"), \
                mock.patch.object(rit, "get_project_root", return_value=Path(".")), \
                mock.patch.object(rit, "run_pg", return_value=(exit_code, skipped)), \
                mock.patch.object(rit, "parse_output", return_value=stats), \
                mock.patch.object(rit, "print_results"), \
                mock.patch.object(rit, "extract_failed_test_output", return_value=[]):
            return rit.main()

    def test_clean_run_with_workflows_passes(self):
        self.assertEqual(self._main_with(0, None), 0)

    def test_skipped_workflows_fail_even_when_everything_that_ran_passed(self):
        # The exact shape of #601: go test exit 0, zero failed tests, zero
        # failed packages -- and the end-to-end suite never ran.
        self.assertNotEqual(self._main_with(0, "test server did not become ready"), 0)

    def test_skip_reason_is_reported(self):
        with mock.patch.object(rit, "log_error") as log_error:
            self._main_with(0, "OAuth stub not available")
        self.assertTrue(
            any("OAuth stub not available" in str(c) for c in log_error.call_args_list),
            "the reason for the skip must reach the operator, not just a non-zero exit",
        )

    def test_real_failure_still_reports_its_own_exit_code(self):
        # A skip must not mask or override a genuine test failure's exit code.
        self.assertEqual(self._main_with(3, "test server did not become ready"), 3)

    def test_failed_tests_still_fail_without_a_skip(self):
        stats = {"passed": 20, "failed": 2, "skipped": 0, "pkg_pass": 3, "pkg_fail": 1}
        self.assertNotEqual(self._main_with(0, None, stats), 0)


class TestRunnersReturnAPair(unittest.TestCase):
    """main() unpacks two values from either runner; a runner that regressed to
    returning a bare int would raise TypeError at the call site instead."""

    def test_run_pg_and_run_oci_are_annotated_as_pairs(self):
        for fn in (rit.run_pg, rit.run_oci):
            self.assertEqual(fn.__annotations__["return"], "tuple[int, str | None]")


if __name__ == "__main__":
    unittest.main()
