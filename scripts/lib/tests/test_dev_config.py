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
            cluster="k3s", file=str(root / "snap.yaml"), config="config.yml", **kw)

    def test_snapshot_uses_explicit_only_export(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            args = self._args(root)
            (root / "snap.yaml").write_text("x")  # makes chmod/relative_to succeed

            with mock.patch.object(dc, "get_project_root", return_value=root), \
                    mock.patch.object(dc, "build_dbtool"), \
                    mock.patch.object(dc, "_dbtool") as run:
                dc.cmd_snapshot(args)
            argv = run.call_args.args[0]
            self.assertIn("--export-config", argv)
            self.assertIn("--explicit-only", argv)
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


if __name__ == "__main__":
    unittest.main()
