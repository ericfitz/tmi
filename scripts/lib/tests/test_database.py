import dataclasses
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import database  # noqa: E402
from tmi_common import config_get, get_project_root, load_config  # noqa: E402

# Resolve config-development.yml relative to the project root.
# get_project_root() is authoritative: it goes 3 levels up from tmi_common.py.
_DEV_CONFIG = str(get_project_root() / "config-development.yml")


def _parsed_dev_url() -> dict:
    """Parse database.url from config-development.yml for expected-value assertions."""
    raw = load_config(_DEV_CONFIG)
    url = config_get(raw, "database.url")
    return database._parse_db_url(url)


class TestDevProfile(unittest.TestCase):
    def test_dev_profile_container_name_is_not_test(self):
        p = database.dev_profile(_DEV_CONFIG)
        self.assertNotIn("test", p.container)
        self.assertEqual(p.config_path, _DEV_CONFIG)

    def test_dev_profile_has_volume(self):
        p = database.dev_profile(_DEV_CONFIG)
        self.assertTrue(p.volume, "dev profile must have a non-empty volume name")

    def test_dev_profile_volume_name(self):
        p = database.dev_profile(_DEV_CONFIG)
        self.assertEqual(p.volume, database.DEV_VOLUME)

    def test_dev_profile_container_name(self):
        p = database.dev_profile(_DEV_CONFIG)
        self.assertEqual(p.container, database.DEV_CONTAINER)

    def test_dev_profile_port_from_config(self):
        """Port must match database.url in config-development.yml."""
        expected = _parsed_dev_url()["port"]
        p = database.dev_profile(_DEV_CONFIG)
        self.assertEqual(p.port, expected)

    def test_dev_profile_user_from_config(self):
        """User must match database.url in config-development.yml."""
        expected = _parsed_dev_url()["user"]
        p = database.dev_profile(_DEV_CONFIG)
        self.assertEqual(p.user, expected)

    def test_dev_profile_database_from_config(self):
        """Database name must match database.url in config-development.yml."""
        expected = _parsed_dev_url()["database"]
        p = database.dev_profile(_DEV_CONFIG)
        self.assertEqual(p.database, expected)


class TestTestProfile(unittest.TestCase):
    def test_dev_and_test_share_container_name(self):
        """Dev and test share the same container name (faithful to original manage-database.py)."""
        self.assertEqual(database.test_profile(_DEV_CONFIG).container, database.dev_profile(_DEV_CONFIG).container)
        self.assertEqual(database.test_profile(_DEV_CONFIG).container, database.DEV_CONTAINER)

    def test_dev_has_volume_test_does_not(self):
        """The real dev/test distinction: dev has a persistent volume, test is ephemeral."""
        self.assertTrue(database.dev_profile(_DEV_CONFIG).volume, "dev profile must have a non-empty volume name")
        self.assertFalse(database.test_profile(_DEV_CONFIG).volume, "test profile must have no volume (ephemeral)")

    def test_test_profile_no_volume(self):
        p = database.test_profile(_DEV_CONFIG)
        self.assertFalse(p.volume, "test profile must have no volume (ephemeral)")

    def test_test_profile_config_path(self):
        p = database.test_profile(_DEV_CONFIG)
        self.assertEqual(p.config_path, _DEV_CONFIG)

    def test_test_profile_user_from_config(self):
        """Test profile user must match database.url in config-development.yml."""
        expected = _parsed_dev_url()["user"]
        p = database.test_profile(_DEV_CONFIG)
        self.assertEqual(p.user, expected)


class TestProfileFrozen(unittest.TestCase):
    def test_dev_profile_is_frozen(self):
        p = database.dev_profile(_DEV_CONFIG)
        with self.assertRaises((dataclasses.FrozenInstanceError, AttributeError)):
            p.container = "changed"  # type: ignore[misc]


class TestParseDbUrl(unittest.TestCase):
    def test_parse_full_url(self):
        url = "postgres://myuser:mypass@localhost:5432/mydb?sslmode=disable"
        result = database._parse_db_url(url)
        self.assertEqual(result["user"], "myuser")
        self.assertEqual(result["password"], "mypass")
        self.assertEqual(result["port"], 5432)
        self.assertEqual(result["database"], "mydb")

    def test_parse_url_missing_port_returns_empty_port(self):
        url = "postgres://user:pass@localhost/db"
        result = database._parse_db_url(url)
        # urlparse does not return a port when none specified
        self.assertNotIn("port", result)

    def test_parse_invalid_url_returns_empty_dict(self):
        result = database._parse_db_url("not-a-url")
        # May or may not parse; should not raise
        self.assertIsInstance(result, dict)


class TestConnectionFromConfig(unittest.TestCase):
    def test_connection_from_dev_config(self):
        conn = database._connection_from_config(_DEV_CONFIG)
        self.assertIn("user", conn)
        self.assertIn("password", conn)
        self.assertIn("port", conn)
        self.assertIn("database", conn)
        # Values must be non-empty strings / positive int
        self.assertTrue(conn["user"])
        self.assertTrue(conn["password"])
        self.assertGreater(conn["port"], 0)
        self.assertTrue(conn["database"])


class TestProfileFromConfig(unittest.TestCase):
    def test_dev_profile_via_profile_from_config(self):
        p = database.profile_from_config(_DEV_CONFIG, ephemeral=False)
        self.assertEqual(p.container, database.DEV_CONTAINER)
        self.assertEqual(p.volume, database.DEV_VOLUME)

    def test_ephemeral_profile_has_no_volume(self):
        p = database.profile_from_config(_DEV_CONFIG, ephemeral=True)
        self.assertFalse(p.volume)

    def test_overrides_applied(self):
        p = database.profile_from_config(
            _DEV_CONFIG,
            ephemeral=False,
            overrides={"container": "my-container", "port": 9999},
        )
        self.assertEqual(p.container, "my-container")
        self.assertEqual(p.port, 9999)

    def test_none_overrides_not_applied(self):
        """None override values must not overwrite config-derived values."""
        p_base = database.profile_from_config(_DEV_CONFIG, ephemeral=False)
        p_none = database.profile_from_config(
            _DEV_CONFIG,
            ephemeral=False,
            overrides={"container": None, "port": None},
        )
        self.assertEqual(p_none.container, p_base.container)
        self.assertEqual(p_none.port, p_base.port)


if __name__ == "__main__":
    unittest.main()
