import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import cluster  # noqa: E402


class TestLocalImageRef(unittest.TestCase):
    def test_local_image_ref_default_tag(self):
        self.assertEqual(cluster.local_image_ref("tmi-server"), "localhost:5000/tmi-server:dev")

    def test_local_image_ref_custom_tag(self):
        self.assertEqual(cluster.local_image_ref("tmi-server", tag="x"), "localhost:5000/tmi-server:x")


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
