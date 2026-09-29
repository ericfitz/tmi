import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import cluster  # noqa: E402


_patcher = None


def setUpModule():
    # Point the k3s loader at the tracked example; tests never read .local/.
    global _patcher
    _patcher = mock.patch.object(cluster, "K3S_CONFIG_FILE", cluster.K3S_EXAMPLE_FILE)
    _patcher.start()


def tearDownModule():
    _patcher.stop()


class TestLocalImageRef(unittest.TestCase):
    def test_local_image_ref_k3s(self):
        self.assertEqual(
            cluster.local_image_ref("tmi-server", cluster="k3s"),
            "k3s-node.example:30500/tmi-server:dev",
        )


class TestRegistryFor(unittest.TestCase):
    def test_registry_for_k3s(self):
        self.assertEqual(cluster.registry_for("k3s"), "k3s-node.example:30500")

    def test_registry_for_unknown_raises(self):
        with self.assertRaises(ValueError):
            cluster.registry_for("kind")


class TestExpectedContext(unittest.TestCase):
    def test_expected_context_default_is_docker_desktop(self):
        self.assertEqual(cluster.expected_context(), "docker-desktop")

    def test_expected_context_k3s(self):
        self.assertEqual(cluster.expected_context("k3s"), "k3s-example")

    def test_expected_context_unknown_raises(self):
        with self.assertRaises(ValueError):
            cluster.expected_context("kind")


class TestDockerDesktopIdentity(unittest.TestCase):
    def test_registry_for_docker_desktop_is_none(self):
        self.assertIsNone(cluster.registry_for("docker-desktop"))

    def test_local_image_ref_docker_desktop_is_bare(self):
        # No registry prefix — the image is imported straight into the node's containerd.
        self.assertEqual(cluster.local_image_ref("tmi-server", cluster="docker-desktop"), "tmi-server:dev")

    def test_local_image_ref_default_is_docker_desktop(self):
        # Default cluster is docker-desktop — no registry prefix.
        self.assertEqual(cluster.local_image_ref("tmi-server"), "tmi-server:dev")

    def test_expected_context_docker_desktop(self):
        self.assertEqual(cluster.expected_context("docker-desktop"), "docker-desktop")

    def test_constants(self):
        self.assertEqual(cluster.DD_CONTEXT, "docker-desktop")
        self.assertEqual(cluster.DD_NODE, "desktop-control-plane")


class TestIsLocalKubeContext(unittest.TestCase):
    def test_is_local_kube_context_kind_prefix(self):
        self.assertTrue(cluster.is_local_kube_context("kind-tmi-dev"))

    def test_is_local_kube_context_known_exact(self):
        self.assertTrue(cluster.is_local_kube_context("docker-desktop"))

    def test_is_local_kube_context_remote_false(self):
        self.assertFalse(cluster.is_local_kube_context("arn:aws:eks:us-east-1:123:cluster/prod"))

    def test_is_local_kube_context_empty_false(self):
        self.assertFalse(cluster.is_local_kube_context(""))


if __name__ == "__main__":
    unittest.main()


class TestK3sConfig(unittest.TestCase):
    def test_example_file_loads(self):
        cfg = cluster.k3s_config()
        self.assertEqual(cfg["node_host"], "k3s-node.example")

    def test_missing_file_is_clear_error_for_k3s_only(self):
        with mock.patch.object(cluster, "K3S_CONFIG_FILE", Path("/nonexistent/k3s.json")):
            with self.assertRaisesRegex(RuntimeError, "not found: cp .*k3s.json.example"):
                cluster.expected_context("k3s")
            # docker-desktop never reads the file
            self.assertEqual(cluster.expected_context("docker-desktop"), "docker-desktop")
            self.assertIsNone(cluster.registry_for("docker-desktop"))

    def test_missing_key_names_key(self):
        with tempfile.TemporaryDirectory() as d:
            f = Path(d) / "k3s.json"
            f.write_text('{"context": "c", "registry": "r"}')
            with mock.patch.object(cluster, "K3S_CONFIG_FILE", f):
                with self.assertRaisesRegex(RuntimeError, "node_host"):
                    cluster.k3s_config()
