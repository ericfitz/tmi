import re
import sys
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import deploy  # noqa: E402

# Repo root: scripts/lib/tests -> up 3 == project root.
_REPO_ROOT = Path(__file__).resolve().parents[3]
_DEV_DIR = _REPO_ROOT / "deployments" / "k8s" / "dev"


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
    def test_overlay_dir_oracle_docker_desktop(self):
        # docker-desktop + oracle uses the dedicated docker-desktop-oracle overlay.
        self.assertTrue(deploy.overlay_dir_for("oracle", "docker-desktop").endswith("/docker-desktop-oracle"))

    def test_overlay_dir_postgres_docker_desktop(self):
        self.assertTrue(deploy.overlay_dir_for("postgres", "docker-desktop").endswith("/docker-desktop"))

    def test_overlay_dir_k3s(self):
        # CLUSTER=k3s uses the k3s overlay regardless of DB flavor.
        self.assertTrue(deploy.overlay_dir_for("postgres", "k3s").endswith("/k3s"))
        self.assertTrue(deploy.overlay_dir_for("oracle", "k3s").endswith("/k3s"))

    def test_overlay_dir_docker_desktop(self):
        self.assertTrue(deploy.overlay_dir_for("postgres", "docker-desktop").endswith("/docker-desktop"))

    def test_overlay_dir_docker_desktop_oracle(self):
        self.assertTrue(deploy.overlay_dir_for("oracle", "docker-desktop").endswith("/docker-desktop-oracle"))

    def test_overlay_dir_docker_desktop_postgres_not_oracle(self):
        p = deploy.overlay_dir_for("postgres", "docker-desktop")
        self.assertTrue(p.endswith("/docker-desktop"))
        self.assertFalse(p.endswith("-oracle"))


class TestInClusterDbHost(unittest.TestCase):
    def test_default_uses_postgres_service(self):
        # docker-desktop is the default cluster target
        self.assertEqual(deploy.in_cluster_db_host(), "postgres")

    def test_k3s_uses_postgres_service(self):
        self.assertEqual(deploy.in_cluster_db_host("k3s"), "postgres")

    def test_docker_desktop_uses_postgres_service(self):
        self.assertEqual(deploy.in_cluster_db_host("docker-desktop"), "postgres")

    def test_k3s_rewrites_url_host_to_postgres_service(self):
        src = 'url: "postgres://tmi_dev:dev123@localhost:5432/tmi_dev?sslmode=disable"'
        out = deploy.rewrite_db_host_for_incluster(src, db_host=deploy.in_cluster_db_host("k3s"))
        self.assertIn("@postgres:5432/tmi_dev", out)


class TestDdBaseImages(unittest.TestCase):
    """The docker-desktop base images pre-imported to dodge the cgr.dev first-run
    pull flake (#517) must stay in sync with the refs in the manifests that
    reference them, or the pre-imported copy won't match what the pods request."""

    def test_includes_postgres_and_redis(self):
        self.assertIn("cgr.dev/chainguard/postgres:latest", deploy.DD_BASE_IMAGES)
        self.assertIn("cgr.dev/chainguard/redis:latest", deploy.DD_BASE_IMAGES)

    def test_postgres_ref_matches_docker_desktop_manifest(self):
        text = (_DEV_DIR / "docker-desktop" / "postgres.yml").read_text()
        self.assertIn("cgr.dev/chainguard/postgres:latest", deploy.DD_BASE_IMAGES)
        self.assertIn("image: cgr.dev/chainguard/postgres:latest", text)

    def test_redis_ref_matches_shared_manifest(self):
        text = (_DEV_DIR / "redis.yml").read_text()
        self.assertIn("cgr.dev/chainguard/redis:latest", deploy.DD_BASE_IMAGES)
        self.assertIn("image: cgr.dev/chainguard/redis:latest", text)

    def test_postgres_flavor_imports_both_base_images(self):
        imgs = deploy.dd_base_images_for("postgres")
        self.assertIn(deploy.DD_POSTGRES_IMAGE, imgs)
        self.assertIn(deploy.DD_REDIS_IMAGE, imgs)

    def test_oracle_flavor_skips_postgres_base_image(self):
        # Oracle uses an external ADB — no in-cluster Postgres pod — so importing
        # the Postgres base image would be wasted work.
        imgs = deploy.dd_base_images_for("oracle")
        self.assertIn(deploy.DD_REDIS_IMAGE, imgs)
        self.assertNotIn(deploy.DD_POSTGRES_IMAGE, imgs)


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


class TestNodePortExposure(unittest.TestCase):
    """Guard the dev-server host-exposure topology (issue #463).

    The server is reached on the host at localhost:8080 via a NodePort published
    by the kind cluster (extraPortMappings), NOT via `kubectl port-forward`.
    These are drift guards: the three places that hard-code the port pair
    (deploy.py constants, the two Service manifests, the kind cluster config)
    must stay in agreement, or the host loses its path to the server.
    """

    def test_constants_are_expected_values(self):
        self.assertEqual(deploy.HOST_PORT, 8080)
        self.assertEqual(deploy.NODE_PORT, 30080)
        self.assertEqual(deploy.SERVER_URL, "http://localhost:8080")

    def _assert_service_is_nodeport(self, manifest_name: str) -> None:
        text = (_DEV_DIR / manifest_name).read_text()
        # Slice the Service document (the second YAML doc, after the '---').
        svc = text.split("\nkind: Service", 1)
        self.assertEqual(len(svc), 2, f"{manifest_name}: no Service document found")
        svc_doc = "kind: Service" + svc[1]
        self.assertRegex(
            svc_doc, r"(?m)^\s*type:\s*NodePort\b",
            f"{manifest_name}: Service must be type NodePort",
        )
        self.assertRegex(
            svc_doc, rf"(?m)^\s*nodePort:\s*{deploy.NODE_PORT}\b",
            f"{manifest_name}: Service nodePort must equal deploy.NODE_PORT",
        )
        self.assertRegex(
            svc_doc, rf"(?m)^\s*-?\s*port:\s*{deploy.HOST_PORT}\b",
            f"{manifest_name}: Service port must equal deploy.HOST_PORT",
        )

    def test_server_service_is_nodeport(self):
        self._assert_service_is_nodeport("server.yml")

    def test_server_oracle_service_is_nodeport(self):
        self._assert_service_is_nodeport("server-oracle.yml")

    def test_server_port_forward_is_k3s_only(self):
        """#463: the KIND server is reached via the NodePort, never a port-forward
        (the userspace proxy collapsed under CATS load). A server port-forward
        exists ONLY for no-own-cluster targets (k3s, docker-desktop) — which have
        no extraPortMappings — and every invocation is gated on the tuple check
        cluster_target in ('k3s', 'docker-desktop')."""
        src = (Path(deploy.__file__)).read_text()
        # Exactly one server port-forward command, inside start_server_port_forward.
        cmd_lines = re.findall(r'port-forward".*svc/tmi-server', src)
        self.assertEqual(len(cmd_lines), 1,
                         "exactly one server port-forward command (the no-own-cluster helper)")
        # Every call site (excluding the def) must be immediately gated on the tuple.
        call_sites = re.findall(r"(?<!def )start_server_port_forward\(\)", src)
        guarded = re.findall(
            r'if cluster_target in \("k3s", "docker-desktop"\):\n\s+start_server_port_forward\(\)',
            src,
        )
        self.assertGreaterEqual(len(call_sites), 1)
        self.assertEqual(
            len(call_sites), len(guarded),
            "every start_server_port_forward() call must be gated on "
            "cluster_target in ('k3s', 'docker-desktop')",
        )
        self.assertIn("svc/redis", src, "deploy.py should still forward redis")

    def test_server_port_forward_gated_for_docker_desktop_too(self):
        src = (Path(deploy.__file__)).read_text()
        # Both no-own-cluster targets gate the server port-forward together.
        self.assertIn('if cluster_target in ("k3s", "docker-desktop"):', src)

    def test_server_port_forward_is_self_healing(self):
        """A userspace port-forward dies when its backing pod rolls; to keep
        localhost:8080 usable for the LIFE of the dev env (not just the instant
        dev-up finishes), the forward runs under a re-launching supervisor loop
        in its own session, and teardown signals the whole group so the kubectl
        child dies with the supervisor shell."""
        src = (Path(deploy.__file__)).read_text()
        self.assertIn("while true", src,
                      "port-forward must run under a re-launching supervisor loop")
        self.assertIn("start_new_session=True", src,
                      "supervised forward must run in its own session for group teardown")
        self.assertIn("killpg", src,
                      "teardown must signal the process group to stop the kubectl child")


class TestServerRolloutTimeout(unittest.TestCase):
    """Rollout-status timeout must be long enough for a fresh Oracle ADB's
    first AutoMigrate, which can take 10-20 min (#479/#480)."""

    def test_oracle_gets_long_budget(self):
        self.assertEqual(deploy.server_rollout_timeout("oracle"), "1200s")

    def test_postgres_keeps_short_budget(self):
        self.assertEqual(deploy.server_rollout_timeout("postgres"), "180s")


class TestServerStartupProbe(unittest.TestCase):
    """Both server manifests must carry a startupProbe so a slow first-boot
    migration is not killed by the livenessProbe mid-flight (#479)."""

    def _assert_has_startup_probe(self, manifest_name: str) -> None:
        text = (_DEV_DIR / manifest_name).read_text()
        # Slice the Deployment document (before the Service '---').
        deploy_doc = text.split("\nkind: Service", 1)[0]
        self.assertRegex(
            deploy_doc, r"(?m)^\s*startupProbe:",
            f"{manifest_name}: Deployment must define a startupProbe",
        )
        # A generous budget: failureThreshold must be large (>= 60) so the
        # first remote migration is not cut short.
        m = re.search(r"startupProbe:.*?failureThreshold:\s*(\d+)", deploy_doc, re.DOTALL)
        self.assertIsNotNone(m, f"{manifest_name}: startupProbe missing failureThreshold")
        self.assertGreaterEqual(
            int(m.group(1)), 60,
            f"{manifest_name}: startupProbe failureThreshold too small for first-boot migration",
        )

    def test_postgres_manifest_has_startup_probe(self):
        self._assert_has_startup_probe("server.yml")

    def test_oracle_manifest_has_startup_probe(self):
        self._assert_has_startup_probe("server-oracle.yml")


class TestRewriteDbHostForIncluster(unittest.TestCase):
    """The in-cluster server reaches the host Postgres via host.docker.internal,
    while config-development.yml keeps localhost for host-side tools (issue #463)."""

    def test_rewrites_localhost_in_postgres_url(self):
        src = 'url: "postgres://tmi_dev:dev123@localhost:5432/tmi_dev?sslmode=disable"'
        out = deploy.rewrite_db_host_for_incluster(src)
        self.assertIn("@host.docker.internal:5432/tmi_dev", out)
        self.assertNotIn("@localhost:", out)

    def test_rewrites_127_0_0_1_in_postgres_url(self):
        src = 'url: "postgres://u:p@127.0.0.1:5432/db"'
        out = deploy.rewrite_db_host_for_incluster(src)
        self.assertIn("@host.docker.internal:5432/db", out)

    def test_leaves_other_localhost_references_untouched(self):
        src = (
            'database:\n'
            '  url: "postgres://tmi_dev:dev123@localhost:5432/tmi_dev?sslmode=disable"\n'
            '  redis:\n'
            '    host: localhost\n'
            'auth:\n'
            '  oauth:\n'
            '    client_callback_allowlist:\n'
            '      - http://localhost:8079/\n'
        )
        out = deploy.rewrite_db_host_for_incluster(src)
        # Only the postgres URL host changed; redis + OAuth callback localhost remain.
        self.assertIn("@host.docker.internal:5432/tmi_dev", out)
        self.assertIn("host: localhost", out)
        self.assertIn("http://localhost:8079/", out)

    def test_noop_on_non_postgres_url(self):
        src = 'url: "oracle://ADMIN@tmiadb_tp"'
        self.assertEqual(deploy.rewrite_db_host_for_incluster(src), src)

    def test_noop_when_host_already_explicit(self):
        src = 'url: "postgres://u:p@db.example.com:5432/db"'
        self.assertEqual(deploy.rewrite_db_host_for_incluster(src), src)

    def test_config_development_yml_uses_localhost(self):
        """The on-disk dev config must keep localhost so host tools work."""
        cfg = (_REPO_ROOT / "config-development.yml").read_text()
        self.assertRegex(cfg, r"postgres://[^\"'\s]*@localhost:5432")
        self.assertNotRegex(cfg, r"postgres://[^\"'\s]*@host\.docker\.internal")


class TestSaveImportCmds(unittest.TestCase):
    def test_builds_docker_save_and_ctr_import_pair(self):
        save, imp = deploy.save_import_cmds("tmi-server:dev", "desktop-control-plane")
        self.assertEqual(save, ["docker", "save", "tmi-server:dev"])
        self.assertEqual(
            imp,
            ["docker", "exec", "-i", "desktop-control-plane",
             "ctr", "-n", "k8s.io", "images", "import", "-"],
        )


class TestImportImageToNode(unittest.TestCase):
    """#519: if the importer Popen raises before we release the saver's stdout,
    the saver must be torn down (stdout closed + killed + waited) so it can't
    deadlock writing into a pipe with no reader — rather than left to hang."""

    def test_importer_popen_raises_tears_down_saver(self):
        saver = mock.MagicMock()
        saver.returncode = 0

        def popen_side_effect(*_args, **_kwargs):
            # First call (docker save) succeeds; second call (ctr import) fails to
            # spawn, e.g. FileNotFoundError if docker exec were unavailable.
            popen_side_effect.calls += 1
            if popen_side_effect.calls == 1:
                return saver
            raise FileNotFoundError("docker exec not found")
        popen_side_effect.calls = 0

        with mock.patch.object(deploy.subprocess, "Popen", side_effect=popen_side_effect):
            with self.assertRaises(FileNotFoundError):
                deploy.import_image_to_node("tmi-server:dev", "desktop-control-plane")

        saver.stdout.close.assert_called_once()   # pipe read end released
        saver.kill.assert_called_once()           # saver stopped, can't block
        saver.wait.assert_called_once()           # reaped in the finally


if __name__ == "__main__":
    unittest.main()
