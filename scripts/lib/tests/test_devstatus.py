import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import devstatus  # noqa: E402


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


if __name__ == "__main__":
    unittest.main()
