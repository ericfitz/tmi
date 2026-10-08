import base64
import importlib.util
import os
import stat
import subprocess
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


# SEM@5149987ee2172bd73af1ed0a0727c09bdedfa562: validate delivering the dev settings key to dbtool via a private file-provider dir
class TestSettingsKeyEnv(unittest.TestCase):
    # SEM@5149987ee2172bd73af1ed0a0727c09bdedfa562: validate key files, perms, env, and cleanup
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

    # SEM@5149987ee2172bd73af1ed0a0727c09bdedfa562: validate fallback when the Secret lacks the key
    def test_missing_key_yields_none_and_warns(self):
        with mock.patch.object(dc, "kubectl", _fake_kubectl({})), \
                mock.patch.object(dc, "log_warn") as warn, dc.settings_key_env() as env:
            self.assertIsNone(env)
        self.assertIn("TMI_SECRET_SETTINGS_ENCRYPTION_KEY", warn.call_args[0][0])

    # SEM@5149987ee2172bd73af1ed0a0727c09bdedfa562: validate the umask is restored
    def test_umask_restored(self):
        before = os.umask(0o022)
        try:
            with mock.patch.object(dc, "kubectl", _fake_kubectl({})), dc.settings_key_env():
                pass
            self.assertEqual(os.umask(0o022), 0o022)
        finally:
            os.umask(before)


# SEM@5149987ee2172bd73af1ed0a0727c09bdedfa562: validate _dbtool passes the key env only when asked
class TestDbtoolEnv(unittest.TestCase):
    # SEM@5149987ee2172bd73af1ed0a0727c09bdedfa562: validate export passes env, restore does not
    def test_env_only_with_flag(self):
        with mock.patch.object(dc, "set_active_context"), \
                mock.patch.object(dc, "ensure_port_forward"), \
                mock.patch.object(dc, "run_cmd") as run, \
                mock.patch.object(dc, "kubectl", _fake_kubectl({"TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "ab"})):
            dc._dbtool(["x"], "k3s", with_settings_key=True)
            self.assertEqual(run.call_args.kwargs["env"]["TMI_SECRETS_PROVIDER"], "file")
            dc._dbtool(["x"], "k3s")
            self.assertNotIn("env", run.call_args.kwargs)


if __name__ == "__main__":
    unittest.main()
