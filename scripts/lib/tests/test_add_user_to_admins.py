"""Tests for scripts/add-user-to-admins.py argument handling.

The target user's identity must come from the operator: there is no
hardcoded default email.
"""

import contextlib
import importlib.util
import io
import sys
import types
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[2] / "add-user-to-admins.py"


def _load_script():
    # The script imports requests at module level; stub it so the test needs no
    # third-party packages and makes no network calls.
    sys.modules.setdefault("requests", types.ModuleType("requests"))
    spec = importlib.util.spec_from_file_location("add_user_to_admins", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ParseArgsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.mod = _load_script()

    def _fails(self, argv):
        with (
            contextlib.redirect_stderr(io.StringIO()) as err,
            self.assertRaises(SystemExit) as cm,
        ):
            self.mod.parse_args(argv)
        self.assertEqual(cm.exception.code, 2)
        return err.getvalue()

    def test_email_is_required(self):
        self.assertIn("--email", self._fails([]))

    def test_rejects_non_email(self):
        self.assertIn("must be an email address", self._fails(["--email", "alice"]))

    def test_rejects_padded_email(self):
        self._fails(["--email", " a@example.com"])

    def test_defaults_provider_to_google(self):
        args = self.mod.parse_args(["--email", "a@example.com"])
        self.assertEqual((args.email, args.provider), ("a@example.com", "google"))

    def test_provider_override(self):
        args = self.mod.parse_args(["--email", "a@example.com", "--provider", "tmi"])
        self.assertEqual(args.provider, "tmi")

    def test_no_hardcoded_email(self):
        self.assertFalse(hasattr(self.mod, "TARGET_EMAIL"))


if __name__ == "__main__":
    unittest.main()
