import argparse
import base64
import importlib.util
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

_p = Path(__file__).resolve().parents[2] / "dev-config.py"
_spec = importlib.util.spec_from_file_location("dev_config", _p)
dc = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dc)


def _fake_kubectl(secrets):
    """kubectl stub: answers jsonpath={.data.<KEY>} from a dict of plaintext values."""

    def run(args, check=True, capture=False, input_text=None):
        key = args[-1].split(".data.")[1].rstrip("}")
        val = secrets.get(key)
        rc = 0 if val is not None else 1
        out = base64.b64encode(val.encode()).decode() if val is not None else ""
        return subprocess.CompletedProcess(args, rc, stdout=out, stderr="")

    return run


class TestSettingsKeyEnv(unittest.TestCase):
    def test_writes_private_files_and_cleans_up(self):
        secrets = {
            "TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "k" * 64,
            "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID": "1",
        }
        with mock.patch.object(dc, "kubectl", _fake_kubectl(secrets)), dc.settings_key_env() as env:
            d = Path(env["TMI_SECRETS_FILE_DIR"])
            self.assertEqual(env["TMI_SECRETS_PROVIDER"], "file")
            self.assertEqual(stat.S_IMODE(d.stat().st_mode), 0o700)
            self.assertEqual(sorted(p.name for p in d.iterdir()),
                             ["settings_encryption_context_id", "settings_encryption_key"])
            for p in d.iterdir():
                self.assertEqual(stat.S_IMODE(p.stat().st_mode), 0o600)
            self.assertEqual((d / "settings_encryption_key").read_text(), "k" * 64)
            # no secret value in the env handed to dbtool
            self.assertNotIn("k" * 64, "".join(env.values()))
        self.assertFalse(d.exists())

    def test_missing_key_yields_none_and_warns(self):
        with mock.patch.object(dc, "kubectl", _fake_kubectl({})), \
                mock.patch.object(dc, "log_warn") as warn, dc.settings_key_env() as env:
            self.assertIsNone(env)
        self.assertIn("TMI_SECRET_SETTINGS_ENCRYPTION_KEY", warn.call_args[0][0])

    def test_umask_restored(self):
        before = os.umask(0o022)
        try:
            with mock.patch.object(dc, "kubectl", _fake_kubectl({})), dc.settings_key_env():
                pass
            self.assertEqual(os.umask(0o022), 0o022)
        finally:
            os.umask(before)


class TestDbtoolEnv(unittest.TestCase):
    def test_env_only_with_flag(self):
        with mock.patch.object(dc, "set_active_context"), \
                mock.patch.object(dc, "ensure_port_forward"), \
                mock.patch.object(dc, "run_cmd") as run, \
                mock.patch.object(dc, "kubectl", _fake_kubectl({"TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "ab"})):
            dc._dbtool(["x"], "k3s", with_settings_key=True)
            self.assertEqual(run.call_args.kwargs["env"]["TMI_SECRETS_PROVIDER"], "file")
            dc._dbtool(["x"], "k3s")
            self.assertNotIn("env", run.call_args.kwargs)


class TestSnapshotRestoreModes(unittest.TestCase):
    """#1180: snapshot exports explicit rows only; restore uses the restore mode."""

    def _args(self, root, **kw):
        return argparse.Namespace(
            cluster="k3s", file=str(root / "snap.yaml"), config="config.yml",
            **{"force": False, **kw})

    def test_snapshot_uses_explicit_only_export(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            args = self._args(root)

            def fake(argv, cluster, with_settings_key=False):
                Path(argv[argv.index("--output") + 1]).write_text("a: 1\n")

            with mock.patch.object(dc, "get_project_root", return_value=root), \
                    mock.patch.object(dc, "build_dbtool"), \
                    mock.patch.object(dc, "_dbtool", side_effect=fake) as run:
                dc.cmd_snapshot(args)
            argv = run.call_args.args[0]
            self.assertIn("--export-config", argv)
            self.assertIn("--explicit-only", argv)
            self.assertIn("--allow-skips", argv)
            self.assertTrue(run.call_args.kwargs["with_settings_key"])

    def test_restore_uses_restore_snapshot_not_import_config(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            args = self._args(root)
            (root / "snap.yaml").write_text("x")
            with mock.patch.object(dc, "build_dbtool"), \
                    mock.patch.object(dc, "_dbtool") as run:
                dc.cmd_restore(args)
            argv = run.call_args.args[0]
            self.assertIn("--restore-snapshot", argv)
            self.assertNotIn("--import-config", argv)
            self.assertNotIn("--overwrite", argv)
            self.assertEqual(argv[argv.index("-f") + 1], str(root / "snap.yaml"))

    def test_restore_without_snapshot_exits(self):
        with tempfile.TemporaryDirectory() as d:
            args = self._args(Path(d))
            with mock.patch.object(dc, "build_dbtool") as build, \
                    mock.patch.object(dc, "_dbtool") as run, \
                    mock.patch.object(dc, "log_error"), \
                    self.assertRaises(SystemExit):
                dc.cmd_restore(args)
            build.assert_not_called()
            run.assert_not_called()

    def test_overwrite_flag_is_gone(self):
        with mock.patch("sys.argv", ["dev-config.py", "restore", "--overwrite"]), \
                mock.patch("sys.stderr"), self.assertRaises(SystemExit):
            dc.parse_args()


class TestSnapshotSafety(unittest.TestCase):
    """The good snapshot must survive an export of a database with no explicit rows."""

    GOOD = "# tmi-dbtool-snapshot: format=explicit-only/v1\noperator:\n  name: Eric\n"
    EMPTY = "# tmi-dbtool-snapshot: format=explicit-only/v1\n"

    def _snap(self, root, new_content, existing=None, prev=None, force=False):
        out = root / "snap.yaml"
        if existing is not None:
            out.write_text(existing)
        if prev is not None:
            (root / "snap.yaml.prev").write_text(prev)

        def fake_dbtool(argv, cluster, with_settings_key=False):
            self.assertIn("--explicit-only", argv)
            Path(argv[argv.index("--output") + 1]).write_text(new_content)

        args = argparse.Namespace(cluster="k3s", file=str(out), config="config.yml", force=force)
        with mock.patch.object(dc, "get_project_root", return_value=root), \
                mock.patch.object(dc, "build_dbtool"), \
                mock.patch.object(dc, "_dbtool", side_effect=fake_dbtool):
            dc.cmd_snapshot(args)
        return out

    def test_empty_export_keeps_good_snapshot_and_prev(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            out = self._snap(root, self.EMPTY, existing=self.GOOD, prev="older")
            self.assertEqual(out.read_text(), self.GOOD)
            self.assertEqual((root / "snap.yaml.prev").read_text(), "older")
            self.assertFalse((root / "snap.yaml.new").exists())

    def test_old_format_good_file_also_kept_over_empty_export(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            out = self._snap(root, self.EMPTY, existing="operator:\n  name: Eric\n")
            self.assertEqual(out.read_text(), "operator:\n  name: Eric\n")

    def test_normal_rotation(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            new = self.GOOD.replace("Eric", "Bob")
            out = self._snap(root, new, existing=self.GOOD, prev="older")
            self.assertEqual(out.read_text(), new)
            prev = root / "snap.yaml.prev"
            self.assertEqual(prev.read_text(), self.GOOD)
            self.assertEqual(stat.S_IMODE(prev.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(out.stat().st_mode), 0o600)
            self.assertFalse((root / "snap.yaml.new").exists())

    def test_first_snapshot_even_if_empty(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            out = self._snap(root, self.EMPTY)
            self.assertEqual(out.read_text(), self.EMPTY)
            self.assertFalse((root / "snap.yaml.prev").exists())

    def test_missing_key_that_was_skipped_blocks_rotation(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            good = self.GOOD + "auth:\n  jwt:\n    secret: s\n"
            partial = self.GOOD + "# skipped-explicit: auth.jwt.secret\n"
            warns = []
            with mock.patch.object(dc, "log_warn", side_effect=warns.append):
                out = self._snap(root, partial, existing=good, prev="older")
            self.assertEqual(out.read_text(), good)
            self.assertEqual((root / "snap.yaml.prev").read_text(), "older")
            self.assertTrue(any("auth.jwt.secret" in m and "--force" in m for m in warns))

    def test_admin_deleted_row_with_no_skip_entry_rotates(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            good = self.GOOD + "auth:\n  jwt:\n    secret: s\n"
            out = self._snap(root, self.GOOD, existing=good, prev="older")
            self.assertEqual(out.read_text(), self.GOOD)
            self.assertEqual((root / "snap.yaml.prev").read_text(), good)

    def test_empty_export_blocks_even_without_skip_entry(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            out = self._snap(root, self.EMPTY, existing=self.GOOD, prev="older")
            self.assertEqual(out.read_text(), self.GOOD)
            self.assertEqual((root / "snap.yaml.prev").read_text(), "older")

    def test_force_bypasses_the_check(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            out = self._snap(root, self.EMPTY, existing=self.GOOD, prev="older", force=True)
            self.assertEqual(out.read_text(), self.EMPTY)
            self.assertEqual((root / "snap.yaml.prev").read_text(), self.GOOD)

    def test_superset_and_different_set_rotate(self):
        for new in (self.GOOD + "extra: 1\n",
                    self.EMPTY + "other:\n  key: 1\n"):
            with tempfile.TemporaryDirectory() as d:
                root = Path(d)
                out = self._snap(root, new, existing=self.GOOD, prev="older")
                self.assertEqual(out.read_text(), new)
                self.assertEqual((root / "snap.yaml.prev").read_text(), self.GOOD)

    def test_corrupt_existing_moved_aside_prev_untouched(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            out = self._snap(root, self.GOOD, existing="a: [unclosed\n", prev="good-prev")
            self.assertEqual(out.read_text(), self.GOOD)
            self.assertEqual((root / "snap.yaml.prev").read_text(), "good-prev")
            aside = list(root.glob("snap.yaml.corrupt-*"))
            self.assertEqual([p.read_text() for p in aside], ["a: [unclosed\n"])

    def test_prev_replaced_with_private_mode_even_if_old_was_group_readable(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            prev = root / "snap.yaml.prev"
            prev.write_text("old")
            os.chmod(prev, 0o640)
            self._snap(root, self.GOOD.replace("Eric", "Bob"), existing=self.GOOD)
            self.assertEqual(stat.S_IMODE(prev.stat().st_mode), 0o600)
            self.assertEqual(prev.read_text(), self.GOOD)
            self.assertEqual([p.name for p in root.iterdir() if ".tmp-" in p.name], [])

    def test_stale_temp_files_removed_and_skips_reported(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / "snap.yaml.new.tmp-abc").write_text("x")
            (root / "snap.yaml.prev.tmp-abc").write_text("x")
            msgs = []
            with mock.patch.object(dc, "log_warn", side_effect=msgs.append):
                self._snap(root, self.GOOD + "# skipped-explicit: a.b, c.d\n")
            self.assertEqual([p.name for p in root.iterdir() if ".tmp-" in p.name], [])
            self.assertTrue(any("2 explicit rows skipped: a.b, c.d" in m for m in msgs))

    def test_failed_export_leaves_good_file_and_cleans_temp(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            out = root / "snap.yaml"
            out.write_text(self.GOOD)

            def boom(argv, cluster, with_settings_key=False):
                Path(argv[argv.index("--output") + 1]).write_text("partial")
                raise RuntimeError("export failed")

            args = argparse.Namespace(cluster="k3s", file=str(out), config="config.yml", force=False)
            with mock.patch.object(dc, "get_project_root", return_value=root), \
                    mock.patch.object(dc, "build_dbtool"), \
                    mock.patch.object(dc, "_dbtool", side_effect=boom), \
                    self.assertRaises(RuntimeError):
                dc.cmd_snapshot(args)
            self.assertEqual(out.read_text(), self.GOOD)
            self.assertFalse((root / "snap.yaml.new").exists())


if __name__ == "__main__":
    unittest.main()
