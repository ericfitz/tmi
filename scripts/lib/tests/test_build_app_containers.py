"""Unit tests for tmi-client version resolution in scripts/build-app-containers.py.

Covers the precedence rules added for #554: TMI_CLIENT_VERSION, then go.mod,
then the git branch name. go.mod sits ahead of the branch heuristic because it
is the module the build actually links against, and because the branch rule has
no answer at all on a branch without a semver in it -- `main` included, which
previously made scripts/deploy-aws.sh unable to complete from the branch
releases are cut from.

The module under test is a top-level hyphenated script rather than an importable
package, so it is loaded by path. Its module-level code is only imports and
constants (real work is behind a __main__ guard), so importing is side-effect
free.
"""

import importlib.util
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

_SCRIPTS_DIR = Path(__file__).resolve().parents[2]
_MODULE_PATH = _SCRIPTS_DIR / "build-app-containers.py"

sys.path.insert(0, str(_SCRIPTS_DIR))
_spec = importlib.util.spec_from_file_location("build_app_containers", _MODULE_PATH)
bac = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(bac)


class TestGoModClientVersion(unittest.TestCase):
    def _write_go_mod(self, body: str) -> Path:
        d = Path(tempfile.mkdtemp())
        (d / "go.mod").write_text(body)
        return d

    def test_reads_version_from_require_line(self):
        root = self._write_go_mod(
            "module github.com/ericfitz/tmi\n\n"
            "require (\n"
            "\tgithub.com/ericfitz/tmi-clients/go-client-generated/v1_5_0 v0.0.0-20260701025703-ad400e64ba0c\n"
            ")\n"
        )
        self.assertEqual(bac._go_mod_client_version(root), "v1_5_0")

    def test_replace_and_require_naming_same_version_is_not_ambiguous(self):
        # A `replace` directive names the version on both sides, so the raw
        # match list has duplicates. That must not be mistaken for ambiguity.
        root = self._write_go_mod(
            "module github.com/ericfitz/tmi\n\n"
            "replace github.com/ericfitz/tmi-clients/go-client-generated/v1_5_0 => "
            "../tmi-clients/go-client-generated/v1_5_0\n\n"
            "require github.com/ericfitz/tmi-clients/go-client-generated/v1_5_0 v0.0.0-2026\n"
        )
        self.assertEqual(bac._go_mod_client_version(root), "v1_5_0")

    def test_multiple_distinct_versions_is_a_hard_error(self):
        root = self._write_go_mod(
            "require github.com/ericfitz/tmi-clients/go-client-generated/v1_5_0 v0.0.0-1\n"
            "require github.com/ericfitz/tmi-clients/go-client-generated/v2_0_0 v0.0.0-2\n"
        )
        with self.assertRaises(SystemExit):
            bac._go_mod_client_version(root)

    def test_no_tmi_clients_module_returns_empty(self):
        root = self._write_go_mod("module github.com/ericfitz/tmi\n\nrequire foo/bar v1.0.0\n")
        self.assertEqual(bac._go_mod_client_version(root), "")

    def test_missing_go_mod_returns_empty(self):
        self.assertEqual(bac._go_mod_client_version(Path(tempfile.mkdtemp())), "")

    def test_unrelated_version_path_is_not_matched(self):
        # Anchoring on the full module prefix keeps an unrelated vN_N_N path
        # elsewhere in go.mod from being picked up.
        root = self._write_go_mod(
            "module github.com/ericfitz/tmi\n\nrequire example.com/other/v9_9_9 v1.0.0\n"
        )
        self.assertEqual(bac._go_mod_client_version(root), "")

    def test_real_repo_go_mod_resolves(self):
        # Guards against the repo's own go.mod drifting out of the shape the
        # regex expects -- the failure mode this test exists to catch.
        repo_root = Path(__file__).resolve().parents[3]
        self.assertRegex(bac._go_mod_client_version(repo_root), r"^v\d+_\d+_\d+$")


class TestResolveClientVersionPrecedence(unittest.TestCase):
    def setUp(self):
        self.root = Path(tempfile.mkdtemp())
        (self.root / "go.mod").write_text(
            "require github.com/ericfitz/tmi-clients/go-client-generated/v1_5_0 v0.0.0-1\n"
        )

    def _no_env(self):
        """Context manager clearing TMI_CLIENT_VERSION for the duration.

        mock.patch.dict restores the real environment on exit; popping os.environ
        directly would leak across tests and make results order-dependent.
        """
        env = {k: v for k, v in os.environ.items() if k != "TMI_CLIENT_VERSION"}
        return mock.patch.dict(os.environ, env, clear=True)

    def test_env_var_wins_over_go_mod(self):
        with mock.patch.dict(os.environ, {"TMI_CLIENT_VERSION": "v9_9_9"}):
            self.assertEqual(bac._resolve_client_version(self.root), "v9_9_9")

    def test_go_mod_wins_over_branch(self):
        # Branch would say v1_4_0; go.mod says v1_5_0 and must win, because
        # go.mod is what the build actually compiles against.
        with self._no_env(), mock.patch.object(
            bac, "_get_git_branch", return_value="dev/1.4.0"
        ):
            self.assertEqual(bac._resolve_client_version(self.root), "v1_5_0")

    def test_main_branch_resolves_via_go_mod(self):
        # The #554 regression: `main` has no semver, so the branch heuristic
        # cannot answer and the build used to exit(1).
        with self._no_env(), mock.patch.object(
            bac, "_get_git_branch", return_value="main"
        ):
            self.assertEqual(bac._resolve_client_version(self.root), "v1_5_0")

    def test_branch_used_when_go_mod_has_no_client(self):
        root = Path(tempfile.mkdtemp())
        (root / "go.mod").write_text("module github.com/ericfitz/tmi\n")
        with self._no_env(), mock.patch.object(
            bac, "_get_git_branch", return_value="dev/1.4.0"
        ):
            self.assertEqual(bac._resolve_client_version(root), "v1_4_0")

    def test_exits_when_no_source_can_answer(self):
        root = Path(tempfile.mkdtemp())
        (root / "go.mod").write_text("module github.com/ericfitz/tmi\n")
        with self._no_env(), mock.patch.object(
            bac, "_get_git_branch", return_value="main"
        ), self.assertRaises(SystemExit):
            bac._resolve_client_version(root)


class TestBranchToClientVersion(unittest.TestCase):
    def test_dev_branch(self):
        self.assertEqual(bac._branch_to_client_version("dev/1.4.0"), "v1_4_0")

    def test_feature_branch_with_semver(self):
        self.assertEqual(bac._branch_to_client_version("feature/2.0.0-thing"), "v2_0_0")

    def test_main_has_no_semver(self):
        self.assertEqual(bac._branch_to_client_version("main"), "")


if __name__ == "__main__":
    unittest.main()
