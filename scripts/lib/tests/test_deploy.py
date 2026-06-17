import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import deploy  # noqa: E402


class TestImageBuildsFor(unittest.TestCase):
    def test_image_builds_postgres_server_image(self):
        names = [n for n, _df, _a in deploy.image_builds_for("postgres")]
        self.assertEqual(names[0], "tmi-server")
        self.assertIn("tmi-extractor", names)
        self.assertIn("tmi-chunk-embed", names)

    def test_image_builds_oracle_server_image(self):
        names = [n for n, _df, _a in deploy.image_builds_for("oracle")]
        self.assertEqual(names[0], "tmi-server-oracle")

    def test_image_builds_postgres_includes_controller(self):
        names = [n for n, _df, _a in deploy.image_builds_for("postgres")]
        self.assertIn("tmi-component-controller", names)

    def test_image_builds_oracle_includes_workers(self):
        names = [n for n, _df, _a in deploy.image_builds_for("oracle")]
        self.assertIn("tmi-extractor", names)
        self.assertIn("tmi-chunk-embed", names)


class TestOverlayDirFor(unittest.TestCase):
    def test_overlay_dir_oracle(self):
        self.assertTrue(deploy.overlay_dir_for("oracle").endswith("/oracle"))

    def test_overlay_dir_postgres(self):
        self.assertFalse(deploy.overlay_dir_for("postgres").endswith("/oracle"))


class TestNoWorkersFiles(unittest.TestCase):
    def test_no_workers_files_oracle_uses_oracle_server(self):
        self.assertIn("server-oracle.yml", deploy._no_workers_files("oracle"))

    def test_no_workers_files_postgres_uses_plain_server(self):
        self.assertIn("server.yml", deploy._no_workers_files("postgres"))

    def test_no_workers_files_includes_controller_and_redis(self):
        for db in ("postgres", "oracle"):
            files = deploy._no_workers_files(db)
            self.assertIn("controller.yml", files)
            self.assertIn("redis.yml", files)


class TestRenderConfigmapYaml(unittest.TestCase):
    def test_render_configmap_embeds_content_and_hash(self):
        out = deploy.render_configmap_yaml(
            name="cm", namespace="ns", file_key="config.yml", content="a: 1\n",
        )
        self.assertIn("kind: ConfigMap", out)
        self.assertIn("name: cm", out)
        self.assertIn("namespace: ns", out)
        self.assertIn("tmi.dev/config-hash:", out)
        self.assertIn("    a: 1", out)  # 4-space block-scalar indent

    def test_render_configmap_contains_file_key(self):
        out = deploy.render_configmap_yaml(
            name="tmi-server-config", namespace="tmi-platform",
            file_key="config.yml", content="server:\n  port: 8080\n",
        )
        self.assertIn("config.yml: |", out)
        self.assertIn("port: 8080", out)


if __name__ == "__main__":
    unittest.main()
