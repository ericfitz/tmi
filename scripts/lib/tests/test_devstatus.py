import json
import sys
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import deploy      # noqa: E402
import devstatus   # noqa: E402
import cluster     # noqa: E402


_patcher = None


def setUpModule():
    # Point the k3s loader at the tracked example; tests never read .local/.
    global _patcher
    _patcher = mock.patch.object(cluster, "K3S_CONFIG_FILE", cluster.K3S_EXAMPLE_FILE)
    _patcher.start()


def tearDownModule():
    _patcher.stop()


class TestDeploymentReadinessParsesReadyAndDesired(unittest.TestCase):
    def test_parses_ready_and_desired(self):
        payload = json.dumps({"items": [
            {"metadata": {"name": "tmi-server"},
             "spec": {"replicas": 1},
             "status": {"readyReplicas": 1}},
            {"metadata": {"name": "redis"},
             "spec": {"replicas": 1},
             "status": {}},  # zero ready
        ]})
        out = dict((n, (r, d)) for n, r, d in devstatus.deployment_readiness(payload))
        self.assertEqual(out["tmi-server"], (1, 1))
        self.assertEqual(out["redis"], (0, 1))


class TestDeploymentReadinessEmpty(unittest.TestCase):
    def test_empty_list(self):
        self.assertEqual(devstatus.deployment_readiness('{"items": []}'), [])


class TestPrintDashboardPinsRequestedCluster(unittest.TestCase):
    """#955: the dashboard's `kubectl get deploy` must target the CLUSTER the
    caller asked about, not whatever the ambient kubeconfig has selected."""

    def setUp(self):
        saved = deploy._active_context
        self.addCleanup(setattr, deploy, "_active_context", saved)

    def test_queries_deployments_with_the_requested_clusters_context(self):
        with mock.patch.object(deploy, "run_cmd") as run_cmd, \
             mock.patch.object(deploy, "server_http_status", return_value=(True, "200")), \
             mock.patch("devstatus.run_cmd", return_value=mock.Mock(returncode=1)):
            run_cmd.return_value = mock.Mock(returncode=0, stdout='{"items": []}')
            devstatus.print_dashboard(cluster_target="k3s")
        # The deploy query went through deploy.kubectl(), which must carry
        # --context k3s-example (the resolved k3s context), not docker-desktop.
        call_args = [c.args[0] for c in run_cmd.call_args_list]
        self.assertTrue(
            any(a[:3] == ["kubectl", "--context", "k3s-example"] for a in call_args),
            f"expected a kubectl call pinned to --context k3s-example, got {call_args}",
        )


if __name__ == "__main__":
    unittest.main()
