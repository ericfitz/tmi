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
    def test_test_profile_distinct_from_dev(self):
        self.assertNotEqual(database.test_profile().container, database.dev_profile().container)

    def test_test_profile_no_volume(self):
        p = database.test_profile()
        self.assertFalse(p.volume, "test profile must have no volume (ephemeral)")

    def test_test_profile_config_path(self):
        p = database.test_profile()
        self.assertEqual(p.config_path, "config-test.yml")


class TestProfileFrozen(unittest.TestCase):
    def test_dev_profile_is_frozen(self):
        p = database.dev_profile()
        with self.assertRaises(Exception):
            p.container = "changed"  # type: ignore[misc]


if __name__ == "__main__":
    unittest.main()
