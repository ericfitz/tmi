import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import devenv  # noqa: E402


class TestLocalImageRef(unittest.TestCase):
    def test_default_registry_and_tag(self):
        self.assertEqual(devenv.local_image_ref("tmi-server"), "localhost:5000/tmi-server:dev")

    def test_explicit_tag(self):
        self.assertEqual(devenv.local_image_ref("tmi-extractor", tag="x"), "localhost:5000/tmi-extractor:x")


class TestIsLocalKubeContext(unittest.TestCase):
    def test_kind_prefix_is_local(self):
        self.assertTrue(devenv.is_local_kube_context("kind-tmi-platform"))

    def test_k3d_prefix_is_local(self):
        self.assertTrue(devenv.is_local_kube_context("k3d-dev"))

    def test_known_exact_names_local(self):
        for name in ("k3s", "default", "rancher-desktop", "docker-desktop", "minikube"):
            self.assertTrue(devenv.is_local_kube_context(name), name)

    def test_prod_like_context_not_local(self):
        self.assertFalse(devenv.is_local_kube_context("gke_prod-proj_us-east1_tmi"))

    def test_empty_not_local(self):
        self.assertFalse(devenv.is_local_kube_context(""))


class TestContentHash(unittest.TestCase):
    def test_stable_and_short(self):
        h1 = devenv.content_hash("abc")
        h2 = devenv.content_hash("abc")
        self.assertEqual(h1, h2)
        self.assertEqual(len(h1), 12)

    def test_differs_on_change(self):
        self.assertNotEqual(devenv.content_hash("abc"), devenv.content_hash("abd"))


class TestRenderConfigmapYaml(unittest.TestCase):
    def test_contains_name_namespace_and_file_key(self):
        out = devenv.render_configmap_yaml(
            name="tmi-server-config", namespace="tmi-platform",
            file_key="config.yml", content="server:\n  port: 8080\n",
        )
        self.assertIn("name: tmi-server-config", out)
        self.assertIn("namespace: tmi-platform", out)
        self.assertIn("config.yml: |", out)
        self.assertIn("port: 8080", out)

    def test_indents_multiline_content_under_key(self):
        out = devenv.render_configmap_yaml(
            name="c", namespace="n", file_key="f.yml", content="a: 1\nb: 2\n",
        )
        self.assertIn("\n    a: 1", out)
        self.assertIn("\n    b: 2", out)


if __name__ == "__main__":
    unittest.main()
