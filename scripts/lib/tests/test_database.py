import dataclasses
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import database  # noqa: E402


class TestDevProfile(unittest.TestCase):
    def test_dev_profile_container_name_is_not_test(self):
        p = database.dev_profile()
        self.assertNotIn("test", p.container)
        self.assertEqual(p.config_path, "config-development.yml")

    def test_dev_profile_has_volume(self):
        p = database.dev_profile()
        self.assertTrue(p.volume, "dev profile must have a non-empty volume name")

    def test_dev_profile_port(self):
        p = database.dev_profile()
        self.assertEqual(p.port, 5432)


class TestTestProfile(unittest.TestCase):
    def test_dev_and_test_share_container_name(self):
        """Dev and test share the same container name (faithful to original manage-database.py)."""
        self.assertEqual(database.test_profile().container, database.dev_profile().container)
        self.assertEqual(database.test_profile().container, "tmi-postgresql")

    def test_dev_has_volume_test_does_not(self):
        """The real dev/test distinction: dev has a persistent volume, test is ephemeral."""
        self.assertTrue(database.dev_profile().volume, "dev profile must have a non-empty volume name")
        self.assertFalse(database.test_profile().volume, "test profile must have no volume (ephemeral)")

    def test_test_profile_no_volume(self):
        p = database.test_profile()
        self.assertFalse(p.volume, "test profile must have no volume (ephemeral)")

    def test_test_profile_config_path(self):
        p = database.test_profile()
        self.assertEqual(p.config_path, "config-test.yml")


class TestProfileFrozen(unittest.TestCase):
    def test_dev_profile_is_frozen(self):
        p = database.dev_profile()
        with self.assertRaises((dataclasses.FrozenInstanceError, AttributeError)):
            p.container = "changed"  # type: ignore[misc]


if __name__ == "__main__":
    unittest.main()
