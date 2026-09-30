import base64
import json
import os
import re
import sys
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import deploy  # noqa: E402
import cluster  # noqa: E402


_patcher = None


# SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: set up module-level test fixtures for deploy tests
def setUpModule():
    # Point the k3s loader at the tracked example; tests never read .local/.
    global _patcher
    _patcher = mock.patch.object(cluster, "K3S_CONFIG_FILE", cluster.K3S_EXAMPLE_FILE)
    _patcher.start()


# SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: tear down module-level test fixtures for deploy tests
def tearDownModule():
    _patcher.stop()

# Repo root: scripts/lib/tests -> up 3 == project root.
_REPO_ROOT = Path(__file__).resolve().parents[3]
_DEV_DIR = _REPO_ROOT / "deployments" / "k8s" / "dev"


# SEM@853c02e236aeede6adf0dddac14262a5c86a314d: test group for ImageBuildsFor behavior
class TestImageBuildsFor(unittest.TestCase):
    # SEM@853c02e236aeede6adf0dddac14262a5c86a314d: test that image builds postgres server image
    def test_image_builds_postgres_server_image(self):
        names = [n for n, _df, _a in deploy.image_builds_for("postgres")]
        self.assertEqual(names[0], "tmi-server")
        self.assertIn("tmi-extractor", names)
        self.assertIn("tmi-chunk-embed", names)

    # SEM@853c02e236aeede6adf0dddac14262a5c86a314d: test that image builds oracle server image
    def test_image_builds_oracle_server_image(self):
        names = [n for n, _df, _a in deploy.image_builds_for("oracle")]
        self.assertEqual(names[0], "tmi-server-oracle")

    # SEM@853c02e236aeede6adf0dddac14262a5c86a314d: test that image builds postgres includes controller
    def test_image_builds_postgres_includes_controller(self):
        names = [n for n, _df, _a in deploy.image_builds_for("postgres")]
        self.assertIn("tmi-component-controller", names)

    # SEM@853c02e236aeede6adf0dddac14262a5c86a314d: test that image builds oracle includes workers
    def test_image_builds_oracle_includes_workers(self):
        names = [n for n, _df, _a in deploy.image_builds_for("oracle")]
        self.assertIn("tmi-extractor", names)
        self.assertIn("tmi-chunk-embed", names)


# SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test group for OverlayDirFor behavior
class TestOverlayDirFor(unittest.TestCase):
    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test that overlay dir oracle docker desktop
    def test_overlay_dir_oracle_docker_desktop(self):
        # docker-desktop + oracle uses the dedicated docker-desktop-oracle overlay.
        self.assertTrue(deploy.overlay_dir_for("oracle", "docker-desktop").endswith("/docker-desktop-oracle"))

    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test that overlay dir postgres docker desktop
    def test_overlay_dir_postgres_docker_desktop(self):
        self.assertTrue(deploy.overlay_dir_for("postgres", "docker-desktop").endswith("/docker-desktop"))

    # SEM@98062f8f1e16295b1a61757cb49580f728643f52: test that overlay dir k3s
    def test_overlay_dir_k3s(self):
        # CLUSTER=k3s uses the k3s overlay regardless of DB flavor.
        self.assertTrue(deploy.overlay_dir_for("postgres", "k3s").endswith("/k3s"))
        self.assertTrue(deploy.overlay_dir_for("oracle", "k3s").endswith("/k3s"))

    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test that overlay dir docker desktop
    def test_overlay_dir_docker_desktop(self):
        self.assertTrue(deploy.overlay_dir_for("postgres", "docker-desktop").endswith("/docker-desktop"))

    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test that overlay dir docker desktop oracle
    def test_overlay_dir_docker_desktop_oracle(self):
        self.assertTrue(deploy.overlay_dir_for("oracle", "docker-desktop").endswith("/docker-desktop-oracle"))

    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test that overlay dir docker desktop postgres not oracle
    def test_overlay_dir_docker_desktop_postgres_not_oracle(self):
        p = deploy.overlay_dir_for("postgres", "docker-desktop")
        self.assertTrue(p.endswith("/docker-desktop"))
        self.assertFalse(p.endswith("-oracle"))


# SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test group for InClusterDbHost behavior
class TestInClusterDbHost(unittest.TestCase):
    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test that default uses postgres service
    def test_default_uses_postgres_service(self):
        # docker-desktop is the default cluster target
        self.assertEqual(deploy.in_cluster_db_host(), "postgres")

    # SEM@98062f8f1e16295b1a61757cb49580f728643f52: test that k3s uses postgres service
    def test_k3s_uses_postgres_service(self):
        self.assertEqual(deploy.in_cluster_db_host("k3s"), "postgres")

    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: test that docker desktop uses postgres service
    def test_docker_desktop_uses_postgres_service(self):
        self.assertEqual(deploy.in_cluster_db_host("docker-desktop"), "postgres")

    # SEM@98062f8f1e16295b1a61757cb49580f728643f52: test that k3s rewrites url host to postgres service
    def test_k3s_rewrites_url_host_to_postgres_service(self):
        src = 'url: "postgres://tmi_dev:dev123@localhost:5432/tmi_dev?sslmode=disable"'
        out = deploy.rewrite_db_host_for_incluster(src, db_host=deploy.in_cluster_db_host("k3s"))
        self.assertIn("@postgres:5432/tmi_dev", out)


# SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: test group for DdBaseImages behavior
class TestDdBaseImages(unittest.TestCase):
    """The docker-desktop base images pre-imported to dodge the cgr.dev first-run
    pull flake (#517) must stay in sync with the refs in the manifests that
    reference them, or the pre-imported copy won't match what the pods request."""

    # SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: test that includes postgres and redis
    def test_includes_postgres_and_redis(self):
        self.assertIn("cgr.dev/chainguard/postgres:latest", deploy.DD_BASE_IMAGES)
        self.assertIn("cgr.dev/chainguard/redis:latest", deploy.DD_BASE_IMAGES)

    # SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: test that postgres ref matches docker desktop manifest
    def test_postgres_ref_matches_docker_desktop_manifest(self):
        text = (_DEV_DIR / "docker-desktop" / "postgres.yml").read_text()
        self.assertIn("cgr.dev/chainguard/postgres:latest", deploy.DD_BASE_IMAGES)
        self.assertIn("image: cgr.dev/chainguard/postgres:latest", text)

    # SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: test that redis ref matches shared manifest
    def test_redis_ref_matches_shared_manifest(self):
        text = (_DEV_DIR / "redis.yml").read_text()
        self.assertIn("cgr.dev/chainguard/redis:latest", deploy.DD_BASE_IMAGES)
        self.assertIn("image: cgr.dev/chainguard/redis:latest", text)

    # SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: test that postgres flavor imports both base images
    def test_postgres_flavor_imports_both_base_images(self):
        imgs = deploy.dd_base_images_for("postgres")
        self.assertIn(deploy.DD_POSTGRES_IMAGE, imgs)
        self.assertIn(deploy.DD_REDIS_IMAGE, imgs)

    # SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: test that oracle flavor skips postgres base image
    def test_oracle_flavor_skips_postgres_base_image(self):
        # Oracle uses an external ADB — no in-cluster Postgres pod — so importing
        # the Postgres base image would be wasted work.
        imgs = deploy.dd_base_images_for("oracle")
        self.assertIn(deploy.DD_REDIS_IMAGE, imgs)
        self.assertNotIn(deploy.DD_POSTGRES_IMAGE, imgs)


# SEM@853c02e236aeede6adf0dddac14262a5c86a314d: test group for RenderConfigmapYaml behavior
class TestRenderConfigmapYaml(unittest.TestCase):
    # SEM@853c02e236aeede6adf0dddac14262a5c86a314d: test that render configmap embeds content and hash
    def test_render_configmap_embeds_content_and_hash(self):
        out = deploy.render_configmap_yaml(
            name="cm", namespace="ns", file_key="config.yml", content="a: 1\n",
        )
        self.assertIn("kind: ConfigMap", out)
        self.assertIn("name: cm", out)
        self.assertIn("namespace: ns", out)
        self.assertIn("tmi.dev/config-hash:", out)
        self.assertIn("    a: 1", out)  # 4-space block-scalar indent

    # SEM@853c02e236aeede6adf0dddac14262a5c86a314d: test that render configmap contains file key
    def test_render_configmap_contains_file_key(self):
        out = deploy.render_configmap_yaml(
            name="tmi-server-config", namespace="tmi-platform",
            file_key="config.yml", content="server:\n  port: 8080\n",
        )
        self.assertIn("config.yml: |", out)
        self.assertIn("port: 8080", out)


# SEM@1a4ca5f99be4a25df66b2836e9b9f4c87628184a: test group for InvalidEnvFileKeys behavior
class TestInvalidEnvFileKeys(unittest.TestCase):
    """Guard the .local/oauth-providers.env validator (issue #791).

    kubectl --from-env-file wants bare KEY=VALUE lines. The likely mistake is
    reaching for the ~/.keys/ convention (`export KEY='value'`), which would
    otherwise be accepted here and produce a Secret with an unusable key name.
    The validator must also never surface a value — these are client secrets.
    """

    # SEM@1a4ca5f99be4a25df66b2836e9b9f4c87628184a: assert env file key validation result (test helper)
    def _check(self, text):
        import tempfile
        with tempfile.NamedTemporaryFile("w", suffix=".env", delete=False) as fh:
            fh.write(text)
        return deploy._invalid_env_file_keys(Path(fh.name))

    # SEM@1a4ca5f99be4a25df66b2836e9b9f4c87628184a: test that accepts bare assignments blanks and comments
    def test_accepts_bare_assignments_blanks_and_comments(self):
        self.assertEqual(self._check(
            "# a comment\n\nOAUTH_PROVIDERS_GOOGLE_ENABLED=true\n"
            "OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET=s3cr3t\n"
        ), [])

    # SEM@1a4ca5f99be4a25df66b2836e9b9f4c87628184a: test that accepts a value containing equals signs
    def test_accepts_a_value_containing_equals_signs(self):
        self.assertEqual(self._check("KEY=abc=def==\n"), [])

    # SEM@1a4ca5f99be4a25df66b2836e9b9f4c87628184a: test that rejects the shell export form
    def test_rejects_the_shell_export_form(self):
        bad = self._check("export OAUTH_PROVIDERS_GOOGLE_CLIENT_ID=abc\n")
        self.assertEqual(len(bad), 1)
        self.assertIn("line 1", bad[0])

    # SEM@1a4ca5f99be4a25df66b2836e9b9f4c87628184a: test that rejects a line with no assignment
    def test_rejects_a_line_with_no_assignment(self):
        bad = self._check("OAUTH_PROVIDERS_GOOGLE_ENABLED\n")
        self.assertEqual(bad, ["line 1 (no '=')"])

    # SEM@1a4ca5f99be4a25df66b2836e9b9f4c87628184a: test that reports the offending line without leaking the value
    def test_reports_the_offending_line_without_leaking_the_value(self):
        bad = self._check("export SECRET_KEY=hunter2-do-not-leak\n")
        self.assertNotIn("hunter2-do-not-leak", " ".join(bad))


# SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate dev server NodePort and port-forward topology stays consistent (drift guard)
class TestNodePortExposure(unittest.TestCase):
    """Guard the dev-server host-exposure topology (issue #463).

    The server is reached on the host at localhost:8080 via a NodePort published
    by the kind cluster (extraPortMappings), NOT via `kubectl port-forward`.
    These are drift guards: the three places that hard-code the port pair
    (deploy.py constants, the two Service manifests, the kind cluster config)
    must stay in agreement, or the host loses its path to the server.
    """

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate deploy host and node port constants have expected values
    def test_constants_are_expected_values(self):
        self.assertEqual(deploy.HOST_PORT, 8080)
        self.assertEqual(deploy.NODE_PORT, 30080)
        self.assertEqual(deploy.SERVER_URL, "http://localhost:8080")

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate a server manifest's Service is a NodePort with expected ports
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

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate the postgres server manifest exposes a NodePort service
    def test_server_service_is_nodeport(self):
        self._assert_service_is_nodeport("server.yml")

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate the oracle server manifest exposes a NodePort service
    def test_server_oracle_service_is_nodeport(self):
        self._assert_service_is_nodeport("server-oracle.yml")

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate server port-forward is only used for no-own-cluster targets
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
        # Every call site (excluding the def) must be guarded. There are two
        # legitimate guard forms:
        #
        #   1. The deploy orchestration paths (dev-up / dev-restart), gated on
        #      cluster_target in ("k3s", "docker-desktop") -- the original #463
        #      invariant.
        #   2. The single dispatch inside ensure_port_forward(), which starts a
        #      forward only when a caller asked for one BY NAME. Its callers
        #      (currently only seeding, via scripts/run-dbtool.py) do not know
        #      cluster_target; they know the server URL is loopback, which is
        #      the equivalent signal. This path is low-volume seeding traffic,
        #      never the fuzzing campaign -- the campaign still hits the
        #      NodePort directly, which is what #463/#578 actually protect.
        #
        # Anything else -- a new, unguarded call site -- must still fail here.
        call_sites = re.findall(r"(?<!def )start_server_port_forward\(\)", src)
        guarded = re.findall(
            r'if cluster_target in \("k3s", "docker-desktop"\):\n\s+start_server_port_forward\(\)',
            src,
        )
        by_name = re.findall(
            r'if name == "server":\n\s+start_server_port_forward\(\)', src,
        )
        self.assertGreaterEqual(len(call_sites), 1)
        self.assertLessEqual(
            len(by_name), 1,
            "only ensure_port_forward() may dispatch the server forward by name",
        )
        self.assertEqual(
            len(call_sites), len(guarded) + len(by_name),
            "every start_server_port_forward() call must be gated on "
            "cluster_target in ('k3s', 'docker-desktop'), or be the single "
            "by-name dispatch inside ensure_port_forward()",
        )
        self.assertIn("svc/redis", src, "deploy.py should still forward redis")

    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: validate server port-forward is gated on k3s and docker-desktop targets
    def test_server_port_forward_gated_for_docker_desktop_too(self):
        src = (Path(deploy.__file__)).read_text()
        # Both no-own-cluster targets gate the server port-forward together.
        self.assertIn('if cluster_target in ("k3s", "docker-desktop"):', src)

    # SEM@aae7b179ab4030c99be2e1d24f576c7e71271707: validate server port-forward runs under a relaunching supervisor with group teardown
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


# SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate on-demand pidfile-tracked postgres port-forward behavior
class TestPostgresPortForward(unittest.TestCase):
    """Seeding opens a direct connection to localhost:5432, so the in-cluster
    Postgres needs a forward. It is on-demand (never started by dev-up, whose
    5432 would collide with a locally installed PostgreSQL) and pidfile-tracked
    so stop_port_forward() tears it down deliberately instead of the legacy
    reaper killing it as an orphan -- the reason a hand-started forward never
    survived a dev-restart."""

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate postgres port-forward command exists and pins the kube context
    def test_postgres_forward_command_exists_and_pins_context(self):
        src = (Path(deploy.__file__)).read_text()
        cmd = re.findall(r'port-forward".*svc/postgres', src)
        self.assertEqual(len(cmd), 1,
                         "exactly one postgres port-forward command")
        # #580: an unpinned kubectl resolves the CURRENT context on every
        # respawn, so a context switch would silently retarget localhost.
        self.assertIn(
            '["kubectl", "--context", ctx, "-n", NS, "port-forward", "svc/postgres"',
            src,
            "postgres forward must pin --context like the redis/server forwards",
        )

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate dev-up never starts the postgres port-forward
    def test_dev_up_does_not_start_postgres_forward(self):
        """It is on-demand only: 5432 collides with a local PostgreSQL install,
        and a developer who never runs CATS should not have to care."""
        src = (Path(deploy.__file__)).read_text()
        wait_body = src.split("def wait_and_forward")[1].split("\ndef ")[0]
        self.assertNotIn("start_postgres_port_forward()", wait_body,
                         "dev-up must not start the postgres forward")

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate stopping port-forwards tears down postgres by pidfile
    def test_stop_port_forward_tears_down_postgres(self):
        src = (Path(deploy.__file__)).read_text()
        stop_body = src.split("def stop_port_forward()")[1].split("\ndef ")[0]
        self.assertIn('_pidfile_path("postgres")', stop_body,
                      "stop_port_forward must tear the postgres forward down by pidfile")

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate each port-forward starter only stops its own pidfile
    def test_each_starter_stops_only_its_own_pidfile(self):
        """The blanket stop_port_forward() used to run inside
        start_redis_port_forward(), which cleared every pidfile and forced an
        ordering constraint between forwards (and destroyed the postgres one on
        every dev-up)."""
        src = (Path(deploy.__file__)).read_text()
        for fn in ("start_redis_port_forward", "start_server_port_forward",
                   "start_postgres_port_forward"):
            body = src.split(f"def {fn}()")[1].split("\ndef ")[0]
            # A bare call statement on its own line -- so a docstring that
            # merely mentions stop_port_forward() in prose does not trip this.
            self.assertIsNone(
                re.search(r"^\s*stop_port_forward\(\)\s*$", body, re.MULTILINE),
                f"{fn} must not call the blanket stop_port_forward()",
            )


# SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate ensure-port-forward starts, reuses, and waits for forwards
class TestEnsurePortForward(unittest.TestCase):
    """ensure_port_forward() is the reusable entry point routines call instead
    of assuming a developer started a forward by hand (modelled on
    tmi_common.ensure_oauth_stub)."""

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate the set of known port-forward names
    def test_known_names(self):
        self.assertEqual(
            set(deploy._FORWARDS), {"server", "redis", "postgres"},
        )

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate ensuring an unknown port-forward name exits
    def test_unknown_name_exits(self):
        with self.assertRaises(SystemExit):
            deploy.ensure_port_forward("nope")

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate a healthy port-forward is left running (idempotent)
    def test_healthy_forward_is_not_restarted(self):
        """Idempotence matters: callers invoke this unconditionally, and it must
        not churn a forward that dev-up already established."""
        started = []
        with mock.patch.object(deploy, "_forward_is_healthy", return_value=True), \
             mock.patch.object(deploy, "start_postgres_port_forward",
                               side_effect=lambda: started.append("postgres")):
            deploy.ensure_port_forward("postgres")
        self.assertEqual(started, [], "a healthy forward must not be restarted")

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate an unhealthy port-forward is started
    def test_unhealthy_forward_is_started(self):
        started = []
        with mock.patch.object(deploy, "_forward_is_healthy", return_value=False), \
             mock.patch.object(deploy, "wait_for_port"), \
             mock.patch.object(deploy, "start_postgres_port_forward",
                               side_effect=lambda: started.append("postgres")):
            deploy.ensure_port_forward("postgres")
        self.assertEqual(started, ["postgres"])

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate starting a port-forward waits for its port to bind
    def test_start_waits_for_the_port_to_bind(self):
        """The supervisor is spawned asynchronously (~3s to bind for postgres),
        so returning right after the starter would hand the caller a forward
        that is not yet usable."""
        with mock.patch.object(deploy, "_forward_is_healthy", return_value=False), \
             mock.patch.object(deploy, "start_postgres_port_forward"), \
             mock.patch.object(deploy, "wait_for_port") as waited:
            deploy.ensure_port_forward("postgres")
        waited.assert_called_once()
        self.assertEqual(waited.call_args.args[0], deploy.POSTGRES_PORT)

    # SEM@2b49b1b7cf41eab60154eaee7ec32fe4d8b0f1ec: validate a healthy port-forward skips waiting for the port
    def test_healthy_forward_does_not_wait(self):
        """The early return must be cheap -- no polling when nothing started."""
        with mock.patch.object(deploy, "_forward_is_healthy", return_value=True), \
             mock.patch.object(deploy, "wait_for_port") as waited:
            deploy.ensure_port_forward("postgres")
        waited.assert_not_called()

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate a port-forward bound to a different kube context is unhealthy
    def test_forward_on_another_context_is_not_healthy(self):
        """#580: a live supervisor pointing localhost at a DIFFERENT cluster is
        worse than none -- it must be replaced, not reused. Compared against
        the process's PINNED active context (#955), not the ambient one."""
        saved = deploy._active_context
        self.addCleanup(setattr, deploy, "_active_context", saved)
        deploy.set_active_context("k3s")
        record = mock.Mock(pid=os.getpid(), context="some-other-context")
        with mock.patch.object(deploy.portfwd, "read_pidfile", return_value=record), \
             mock.patch.object(deploy.portfwd, "port_listeners", return_value=[(1, "x")]):
            self.assertFalse(deploy._forward_is_healthy("postgres"))


# SEM@1f715c04cbd750d89c29f11fa383709275d63044: provide test base class that saves and restores the active kube context
class _ActiveContextTestCase(unittest.TestCase):
    """Base class that saves/restores deploy._active_context around each test,
    since it is process-global module state that tests must not leak between
    each other."""

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: save active kube context and restore it after each test
    def setUp(self):
        super().setUp()
        saved = deploy._active_context
        self.addCleanup(setattr, deploy, "_active_context", saved)


# SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate kubectl always uses the pinned kube context
class TestActiveContextPinning(_ActiveContextTestCase):
    """#955: kubectl() must always carry an explicit --context resolved from
    the caller's own CLUSTER, never the ambient `kubectl config
    current-context` -- the shared, mutable state that let a concurrent
    devenv run for a DIFFERENT cluster race this one (a `dev-down
    CLUSTER=docker-desktop` deleted a concurrent `dev-up CLUSTER=k3s`'s
    workloads)."""

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate kubectl carries the pinned k3s context
    def test_kubectl_uses_context_pinned_for_k3s(self):
        deploy.set_active_context("k3s")
        with mock.patch.object(deploy, "run_cmd") as run_cmd:
            deploy.kubectl(["get", "pods"])
        run_cmd.assert_called_once()
        self.assertEqual(run_cmd.call_args.args[0],
                         ["kubectl", "--context", "k3s-example", "get", "pods"])

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate kubectl carries the pinned docker-desktop context
    def test_kubectl_uses_context_pinned_for_docker_desktop(self):
        deploy.set_active_context("docker-desktop")
        with mock.patch.object(deploy, "run_cmd") as run_cmd:
            deploy.kubectl(["get", "pods"])
        self.assertEqual(run_cmd.call_args.args[0],
                         ["kubectl", "--context", "docker-desktop", "get", "pods"])

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate kubectl raises when no kube context is pinned
    def test_kubectl_fails_loudly_without_a_pinned_context(self):
        """Never silently fall back to the ambient context -- raise instead."""
        deploy._active_context = None
        with self.assertRaises(RuntimeError):
            deploy.kubectl(["get", "pods"])

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate pinning a context for an unknown cluster is rejected
    def test_set_active_context_rejects_unknown_cluster(self):
        with self.assertRaises(ValueError):
            deploy.set_active_context("kind")

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate pinning the ambient context raises when none exists
    def test_pin_ambient_context_fails_loudly_with_no_context(self):
        with mock.patch.object(deploy, "current_kube_context", return_value=""):
            with self.assertRaises(RuntimeError):
                deploy.pin_ambient_context()

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate pinning the ambient context adopts the current kube context
    def test_pin_ambient_context_pins_whatever_is_current(self):
        with mock.patch.object(deploy, "current_kube_context", return_value="some-ambient-ctx"):
            self.assertEqual(deploy.pin_ambient_context(), "some-ambient-ctx")
        self.assertEqual(deploy._require_active_context(), "some-ambient-ctx")


# SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate port-forward pidfile paths are scoped per kube context
class TestPidfilePathIsPerContext(_ActiveContextTestCase):
    """#955: each forward's pidfile path is scoped to the active context, so
    docker-desktop and k3s can never share, read, or clobber each other's
    pidfile -- the mechanism that let a docker-desktop `dev-down` tear down
    k3s's live server/redis/postgres port-forwards."""

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate pidfile paths differ between clusters
    def test_paths_differ_by_cluster(self):
        deploy.set_active_context("k3s")
        k3s_path = deploy._pidfile_path("server")
        deploy.set_active_context("docker-desktop")
        dd_path = deploy._pidfile_path("server")
        self.assertNotEqual(k3s_path, dd_path)
        self.assertIn("k3s-example", k3s_path)
        self.assertIn("docker-desktop", dd_path)
        self.assertNotIn("docker-desktop", k3s_path)
        self.assertNotIn("k3s-example", dd_path)

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate pidfile paths differ per forward kind within a cluster
    def test_kinds_differ_within_one_cluster(self):
        deploy.set_active_context("docker-desktop")
        paths = {deploy._pidfile_path(k) for k in ("server", "redis", "postgres")}
        self.assertEqual(len(paths), 3)

    # SEM@1f715c04cbd750d89c29f11fa383709275d63044: validate pidfile path lookup requires a pinned context
    def test_requires_a_pinned_context(self):
        deploy._active_context = None
        with self.assertRaises(RuntimeError):
            deploy._pidfile_path("server")


# SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate stopping port-forwards only affects the active cluster
class TestStopPortForwardIsClusterScoped(_ActiveContextTestCase):
    """#955, second half of the incident: `teardown() -> stop_port_forward()`
    used to kill the server/redis/postgres port-forwards regardless of
    cluster -- a docker-desktop `dev-down` killed k3s's live forwards. Every
    pidfile stop_port_forward() touches, and the legacy-reaper pattern it
    falls back to, must be scoped to the ACTIVE cluster only."""

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate stop targets only the active context's pidfiles
    def test_stop_only_targets_pidfiles_for_the_active_context(self):
        deploy.set_active_context("docker-desktop")
        stopped_paths = []
        with mock.patch.object(deploy, "_stop_port_forward_pidfile",
                                side_effect=lambda p: stopped_paths.append(p)), \
             mock.patch.object(deploy.portfwd, "reap_supervisors", return_value=[]) as reap:
            deploy.stop_port_forward()

        self.assertEqual(len(stopped_paths), 3, "server, redis, and postgres pidfiles")
        for p in stopped_paths:
            self.assertIn("docker-desktop", p)
            self.assertNotIn("k3s-example", p)

        # The legacy reap pattern must be scoped to THIS cluster's --context,
        # so it can never match a different cluster's supervisor.
        reap.assert_called_once()
        pattern = reap.call_args.args[0]
        self.assertIn("--context docker-desktop", pattern)
        self.assertNotIn("k3s-example", pattern)

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate stopping k3s forwards never touches docker-desktop resources
    def test_stop_for_k3s_never_mentions_docker_desktop(self):
        deploy.set_active_context("k3s")
        stopped_paths = []
        with mock.patch.object(deploy, "_stop_port_forward_pidfile",
                                side_effect=lambda p: stopped_paths.append(p)), \
             mock.patch.object(deploy.portfwd, "reap_supervisors", return_value=[]) as reap:
            deploy.stop_port_forward()

        for p in stopped_paths:
            self.assertIn("k3s-example", p)
            self.assertNotIn("docker-desktop", p)
        pattern = reap.call_args.args[0]
        self.assertIn("--context k3s-example", pattern)
        self.assertNotIn("docker-desktop", pattern)


# SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: validate server rollout timeout budgets per database type
class TestServerRolloutTimeout(unittest.TestCase):
    """Rollout-status timeout must be long enough for a fresh Oracle ADB's
    first AutoMigrate, which can take 10-20 min (#479/#480)."""

    # SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: validate oracle server rollout gets a long timeout
    def test_oracle_gets_long_budget(self):
        self.assertEqual(deploy.server_rollout_timeout("oracle"), "1200s")

    # SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: validate postgres server rollout keeps a short timeout
    def test_postgres_keeps_short_budget(self):
        self.assertEqual(deploy.server_rollout_timeout("postgres"), "180s")


# SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: validate server manifests define a startup probe
class TestServerStartupProbe(unittest.TestCase):
    """Both server manifests must carry a startupProbe so a slow first-boot
    migration is not killed by the livenessProbe mid-flight (#479)."""

    # SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: validate a server manifest's Deployment defines a startup probe
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

    # SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: validate the postgres server manifest defines a startup probe
    def test_postgres_manifest_has_startup_probe(self):
        self._assert_has_startup_probe("server.yml")

    # SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: validate the oracle server manifest defines a startup probe
    def test_oracle_manifest_has_startup_probe(self):
        self._assert_has_startup_probe("server-oracle.yml")


# SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate rewriting database host to the in-cluster host address
class TestRewriteDbHostForIncluster(unittest.TestCase):
    """The in-cluster server reaches the host Postgres via host.docker.internal,
    while config-development.yml keeps localhost for host-side tools (issue #463)."""

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate rewriting localhost in a postgres URL to the in-cluster host
    def test_rewrites_localhost_in_postgres_url(self):
        src = 'url: "postgres://tmi_dev:dev123@localhost:5432/tmi_dev?sslmode=disable"'
        out = deploy.rewrite_db_host_for_incluster(src)
        self.assertIn("@host.docker.internal:5432/tmi_dev", out)
        self.assertNotIn("@localhost:", out)

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate rewriting 127.0.0.1 in a postgres URL to the in-cluster host
    def test_rewrites_127_0_0_1_in_postgres_url(self):
        src = 'url: "postgres://u:p@127.0.0.1:5432/db"'
        out = deploy.rewrite_db_host_for_incluster(src)
        self.assertIn("@host.docker.internal:5432/db", out)

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate non-database localhost references are not rewritten
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

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate rewrite leaves non-postgres URLs unchanged
    def test_noop_on_non_postgres_url(self):
        src = 'url: "oracle://ADMIN@tmiadb_tp"'
        self.assertEqual(deploy.rewrite_db_host_for_incluster(src), src)

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate rewrite leaves explicit database hosts unchanged
    def test_noop_when_host_already_explicit(self):
        src = 'url: "postgres://u:p@db.example.com:5432/db"'
        self.assertEqual(deploy.rewrite_db_host_for_incluster(src), src)

    # SEM@f47410c1792840e1fe5efba9e5e79e4399da033a: validate the on-disk dev config keeps localhost for the database
    def test_config_development_yml_uses_localhost(self):
        """The on-disk dev config must keep localhost so host tools work."""
        cfg = (_REPO_ROOT / "config-development.yml").read_text()
        self.assertRegex(cfg, r"postgres://[^\"'\s]*@localhost:5432")
        self.assertNotRegex(cfg, r"postgres://[^\"'\s]*@host\.docker\.internal")


# SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: validate building image save and import commands
class TestSaveImportCmds(unittest.TestCase):
    # SEM@02dbf9ba4f3e0b9761454c43cbccc2f4f61bc0d4: validate building the docker save and ctr import command pair
    def test_builds_docker_save_and_ctr_import_pair(self):
        save, imp = deploy.save_import_cmds("tmi-server:dev", "desktop-control-plane")
        self.assertEqual(save, ["docker", "save", "tmi-server:dev"])
        self.assertEqual(
            imp,
            ["docker", "exec", "-i", "desktop-control-plane",
             "ctr", "-n", "k8s.io", "images", "import", "-"],
        )


# SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: validate image import tears down the saver process on importer failure
class TestImportImageToNode(unittest.TestCase):
    """#519: if the importer Popen raises before we release the saver's stdout,
    the saver must be torn down (stdout closed + killed + waited) so it can't
    deadlock writing into a pipe with no reader — rather than left to hang."""

    # SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: validate image saver process is torn down when importer fails to spawn
    def test_importer_popen_raises_tears_down_saver(self):
        saver = mock.MagicMock()
        saver.returncode = 0

        # SEM@a9938a8b543f0fdbc240594d11f85b3ce0f03e24: simulate first process spawn succeeding and second failing (test double)
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


# SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate reuse of preinstalled cert-manager and reloader in platform base
class TestReusePreinstalledPlatform(unittest.TestCase):
    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: build a mock kubectl result with return code and output (test helper)
    @staticmethod
    def _res(rc=0, out=""):
        return mock.Mock(returncode=rc, stdout=out)

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate cert-manager webhook lookup returns its namespace and name
    def test_webhook_found_returns_namespace_and_name(self):
        with mock.patch.object(deploy, "kubectl", side_effect=[
                self._res(0, "customresourcedefinition/x"),
                self._res(0, "cert-manager cert-manager-webhook\n")]):
            self.assertEqual(deploy._find_cert_manager_webhook(),
                             ("cert-manager", "cert-manager-webhook"))

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate missing cert-manager CRD is treated as a fresh cluster
    def test_no_crd_means_fresh_cluster(self):
        with mock.patch.object(deploy, "kubectl", return_value=self._res(1)):
            self.assertIsNone(deploy._find_cert_manager_webhook())

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate cert-manager webhook lookup falls back to the second selector
    def test_second_selector_is_the_fallback(self):
        calls = []

        # SEM@663417962552d1b180936cab2f93692cef6cb1c6: simulate kubectl responses for CRD and webhook lookups (test double)
        def fake(args, **_kw):
            calls.append(args)
            if args[:2] == ["get", "crd"]:
                return self._res(0, "crd")
            return self._res(0, "" if len(calls) == 2 else "cert-manager wh\n")
        with mock.patch.object(deploy, "kubectl", side_effect=fake):
            self.assertEqual(deploy._find_cert_manager_webhook(), ("cert-manager", "wh"))
        self.assertEqual(len(calls), 3)
        self.assertIn(deploy.CERT_MANAGER_WEBHOOK_SELECTORS[1], calls[2])

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate platform selectors require Helm management
    def test_selectors_require_helm_management(self):
        for sel in (*deploy.CERT_MANAGER_WEBHOOK_SELECTORS, deploy.RELOADER_SELECTOR):
            self.assertIn("app.kubernetes.io/managed-by=Helm", sel)

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate reloader detection uses only the Helm selector
    def test_reloader_uses_helm_selector_only(self):
        with mock.patch.object(deploy, "kubectl", return_value=self._res(0, "deployment/r")) as k:
            self.assertTrue(deploy._reloader_exists())
        self.assertIn(deploy.RELOADER_SELECTOR, k.call_args[0][0])
        with mock.patch.object(deploy, "kubectl", return_value=self._res(0, "")):
            self.assertFalse(deploy._reloader_exists())

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: apply platform base with mocks and return kubectl commands (test helper)
    def _run_base(self, webhook, reloader):
        with mock.patch.object(deploy, "get_project_root", return_value=_REPO_ROOT), \
             mock.patch.object(deploy, "_find_cert_manager_webhook", return_value=webhook), \
             mock.patch.object(deploy, "_reloader_exists", return_value=reloader), \
             mock.patch.object(deploy, "_apply_with_retry"), \
             mock.patch.object(deploy, "kubectl") as k:
            deploy.apply_platform_base()
        return [c[0][0] for c in k.call_args_list]

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate Helm-installed platform components skip vendored manifest applies
    def test_helm_install_skips_vendored_applies(self):
        cmds = self._run_base(("cert-manager", "cert-manager-webhook"), True)
        flat = [" ".join(c) for c in cmds]
        self.assertFalse(any("cert-manager.yml" in c for c in flat))
        self.assertFalse(any("reloader.yml" in c for c in flat))

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate absent Helm components cause vendored manifests to be applied
    def test_no_helm_install_applies_vendored(self):
        flat = [" ".join(c) for c in self._run_base(None, False)]
        self.assertTrue(any(c.startswith("apply --server-side") and "cert-manager.yml" in c for c in flat))
        self.assertTrue(any("reloader.yml" in c for c in flat))

    # SEM@663417962552d1b180936cab2f93692cef6cb1c6: validate a webhook outside the cert-manager namespace aborts platform apply
    def test_webhook_outside_cert_manager_namespace_fails_fast(self):
        with mock.patch.object(deploy, "get_project_root", return_value=_REPO_ROOT), \
             mock.patch.object(deploy, "_find_cert_manager_webhook", return_value=("kube-system", "wh")), \
             mock.patch.object(deploy, "kubectl") as k:
            with self.assertRaises(SystemExit):
                deploy.apply_platform_base()
        k.assert_not_called()


# SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate overlay apply substitutes the k3s registry placeholder
class TestApplyOverlayRegistrySubstitution(unittest.TestCase):
    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate k3s registry placeholder is replaced before applying overlay
    def test_k3s_placeholder_replaced_before_apply(self):
        rendered = f"image: {deploy.K3S_REGISTRY_PLACEHOLDER}/tmi-server:dev\n"
        run = mock.Mock(return_value=mock.Mock(stdout=rendered))
        with mock.patch.object(deploy, "run_cmd", run), \
             mock.patch.object(deploy, "kubectl") as kc:
            deploy.apply_overlay("postgres", "k3s")
        applied = kc.call_args.kwargs["input_text"]
        self.assertIn("k3s-node.example:30500/tmi-server:dev", applied)
        self.assertNotIn(".invalid", applied)

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: validate registry placeholder appears in tracked k3s overlay files
    def test_placeholder_matches_tracked_overlay(self):
        for rel in ("kustomization.yaml", "patches/extractor-image.yaml", "patches/chunkembed-image.yaml"):
            text = (_DEV_DIR / "k3s" / rel).read_text()
            self.assertIn(deploy.K3S_REGISTRY_PLACEHOLDER, text, rel)


# SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: smoke-run each secret creator with kubectl mocked
class TestSecretCreatorsRun(unittest.TestCase):
    """Smoke-run each secret creator with kubectl mocked, so an undefined name
    (F821) or bad signature in the function body fails a test."""

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: run a secret creator with mocked kubectl and validate the apply call (test helper)
    def _run(self, fn, env=None):
        with mock.patch.object(deploy, "kubectl", return_value=mock.Mock(stdout="kind: Secret\n")) as kc, \
             mock.patch.dict(os.environ, env or {}):
            fn()
        self.assertEqual(kc.call_args.args[0], ["apply", "-f", "-"])
        self.assertEqual(kc.call_args.kwargs["input_text"], "kind: Secret\n")

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: smoke-run the embedding secret creator
    def test_embedding(self):
        self._run(deploy.create_embedding_secret)

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: smoke-run the OAuth providers secret creator
    def test_oauth_providers(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / ".local").mkdir()
            (Path(d) / ".local" / "oauth-providers.env").write_text("FOO=bar\n")
            with mock.patch.object(deploy, "get_project_root", return_value=Path(d)):
                self._run(deploy.create_oauth_providers_secret)

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: smoke-run the oracle wallet secret creator
    def test_oracle_wallet(self):
        import tempfile
        with tempfile.NamedTemporaryFile(suffix=".zip") as f:
            self._run(deploy.create_oracle_wallet_secret, {"TMI_ORACLE_WALLET_ZIP": f.name})

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: smoke-run the oracle database secret creator
    def test_oracle_db(self):
        self._run(deploy.create_oracle_db_secret,
                  {"TMI_DATABASE_URL": "oracle://x", "ORACLE_PASSWORD": "p"})




# SEM@66902813772bbf4671fb38519c2f21a53dfa2b63: validate seeding adds missing secret keys without overwriting
class TestSeedTmiSecretKeys(unittest.TestCase):
    """seed_tmi_secret_keys merges missing keys and never overwrites (#965)."""

    CONFIG = 'database:\n  url: "postgres://u:pw@localhost:5432/d?sslmode=disable"\n'

    # SEM@2004edc9f39fd83ac4fcd7a38fee8b850e69cb95: run secret key seeding with mocked kubectl and return patched data (test helper)
    def _run(self, present, db="postgres"):
        patches = []

        # SEM@66902813772bbf4671fb38519c2f21a53dfa2b63: simulate kubectl secret key presence lookups (test double)
        def fake_kubectl(args, **kw):
            if args[:3] == ["-n", deploy.NS, "get"]:
                key = args[-1].split(".data.")[1].rstrip("}")
                return mock.Mock(returncode=0, stdout="x" if key in present else "")
            if "patch" in args:
                path = [a for a in args if a.startswith("--patch-file=")][0].split("=", 1)[1]
                patches.append(json.loads(Path(path).read_text())["data"])
            return mock.Mock(returncode=0, stdout="")

        cfg = mock.Mock(read_text=lambda: self.CONFIG)
        with mock.patch.object(deploy, "kubectl", side_effect=fake_kubectl), \
             mock.patch.object(deploy, "get_project_root", return_value=mock.MagicMock(__truediv__=lambda s, o: cfg)), \
             mock.patch.object(deploy, "log_success"):
            deploy.seed_tmi_secret_keys("docker-desktop", db)
        return patches

    # SEM@66902813772bbf4671fb38519c2f21a53dfa2b63: validate seeding adds all secret keys when none exist
    def test_adds_all_when_absent(self):
        (data,) = self._run(set())
        self.assertEqual(set(data), {"TMI_SECRET_SETTINGS_ENCRYPTION_KEY",
                                     "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID", "TMI_DATABASE_URL"})
        self.assertEqual(base64.b64decode(data["TMI_DATABASE_URL"]).decode(),
                         "postgres://u:pw@postgres:5432/d?sslmode=disable")
        self.assertEqual(base64.b64decode(data["TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"]), b"1")

    # SEM@66902813772bbf4671fb38519c2f21a53dfa2b63: validate seeding never overwrites existing secret keys
    def test_never_overwrites_existing(self):
        (data,) = self._run({"TMI_SECRET_SETTINGS_ENCRYPTION_KEY", "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"})
        self.assertEqual(set(data), {"TMI_DATABASE_URL"})

    # SEM@66902813772bbf4671fb38519c2f21a53dfa2b63: validate seeding skips patching when all keys exist
    def test_no_patch_when_all_present(self):
        self.assertEqual(self._run({"TMI_SECRET_SETTINGS_ENCRYPTION_KEY",
                                    "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID", "TMI_DATABASE_URL"}), [])

    # SEM@66902813772bbf4671fb38519c2f21a53dfa2b63: validate seeding omits the database URL for oracle
    def test_oracle_skips_database_url(self):
        (data,) = self._run(set(), db="oracle")
        self.assertNotIn("TMI_DATABASE_URL", data)


if __name__ == "__main__":
    unittest.main()
