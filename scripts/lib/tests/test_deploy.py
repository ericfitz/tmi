import base64
import json
import os
import re
import sys
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import cluster
import deploy

_patcher = None


def setUpModule():
    # Point the k3s loader at the tracked example; tests never read .local/.
    global _patcher
    _patcher = mock.patch.object(cluster, "K3S_CONFIG_FILE", cluster.K3S_EXAMPLE_FILE)
    _patcher.start()


def tearDownModule():
    _patcher.stop()

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
        self.assertIn(deploy.DD_POSTGRES_IMAGE, deploy.DD_BASE_IMAGES)
        self.assertIn("cgr.dev/chainguard/redis:latest", deploy.DD_BASE_IMAGES)

    def test_postgres_ref_matches_docker_desktop_manifest(self):
        text = (_DEV_DIR / "docker-desktop" / "postgres.yml").read_text()
        self.assertIn(f"image: {deploy.DD_POSTGRES_IMAGE}", text)

    def test_postgres_ref_matches_k3s_manifest_and_dockerfile(self):
        text = (_DEV_DIR / "k3s" / "postgres.yml").read_text()
        self.assertIn(f"image: {deploy.DD_POSTGRES_IMAGE}", text)
        dockerfile = (_REPO_ROOT / "Dockerfile.postgres").read_text()
        self.assertIn(f"FROM {deploy.DD_POSTGRES_IMAGE}", dockerfile)

    def test_postgres_image_is_digest_pinned(self):
        # A floating tag lets a reschedule pull a new major version that cannot
        # open the existing data directory (PG18 -> PG19 crash loop, 2026-11-25).
        self.assertRegex(deploy.DD_POSTGRES_IMAGE, r"^cgr\.dev/chainguard/postgres@sha256:[0-9a-f]{64}$")
        for manifest in ("k3s", "docker-desktop"):
            text = (_DEV_DIR / manifest / "postgres.yml").read_text()
            self.assertNotIn("postgres:latest", text, manifest)

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


class TestInvalidEnvFileKeys(unittest.TestCase):
    """Guard the .local/oauth-providers.env validator (issue #791).

    kubectl --from-env-file wants bare KEY=VALUE lines. The likely mistake is
    reaching for the ~/.keys/ convention (`export KEY='value'`), which would
    otherwise be accepted here and produce a Secret with an unusable key name.
    The validator must also never surface a value — these are client secrets.
    """

    def _check(self, text):
        import tempfile
        with tempfile.NamedTemporaryFile("w", suffix=".env", delete=False) as fh:
            fh.write(text)
        return deploy._invalid_env_file_keys(Path(fh.name))

    def test_accepts_bare_assignments_blanks_and_comments(self):
        self.assertEqual(self._check(
            "# a comment\n\nOAUTH_PROVIDERS_GOOGLE_ENABLED=true\n"
            "OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET=s3cr3t\n"
        ), [])

    def test_accepts_a_value_containing_equals_signs(self):
        self.assertEqual(self._check("KEY=abc=def==\n"), [])

    def test_rejects_the_shell_export_form(self):
        bad = self._check("export OAUTH_PROVIDERS_GOOGLE_CLIENT_ID=abc\n")
        self.assertEqual(len(bad), 1)
        self.assertIn("line 1", bad[0])

    def test_rejects_a_line_with_no_assignment(self):
        bad = self._check("OAUTH_PROVIDERS_GOOGLE_ENABLED\n")
        self.assertEqual(bad, ["line 1 (no '=')"])

    def test_reports_the_offending_line_without_leaking_the_value(self):
        bad = self._check("export SECRET_KEY=hunter2-do-not-leak\n")
        self.assertNotIn("hunter2-do-not-leak", " ".join(bad))


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


class TestPostgresPortForward(unittest.TestCase):
    """Seeding opens a direct connection to localhost:5432, so the in-cluster
    Postgres needs a forward. It is on-demand (never started by dev-up, whose
    5432 would collide with a locally installed PostgreSQL) and pidfile-tracked
    so stop_port_forward() tears it down deliberately instead of the legacy
    reaper killing it as an orphan -- the reason a hand-started forward never
    survived a dev-restart."""

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

    def test_dev_up_does_not_start_postgres_forward(self):
        """It is on-demand only: 5432 collides with a local PostgreSQL install,
        and a developer who never runs CATS should not have to care."""
        src = (Path(deploy.__file__)).read_text()
        wait_body = src.split("def wait_and_forward")[1].split("\ndef ")[0]
        self.assertNotIn("start_postgres_port_forward()", wait_body,
                         "dev-up must not start the postgres forward")

    def test_stop_port_forward_tears_down_postgres(self):
        src = (Path(deploy.__file__)).read_text()
        stop_body = src.split("def stop_port_forward()")[1].split("\ndef ")[0]
        self.assertIn('_pidfile_path("postgres")', stop_body,
                      "stop_port_forward must tear the postgres forward down by pidfile")

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


class TestEnsurePortForward(unittest.TestCase):
    """ensure_port_forward() is the reusable entry point routines call instead
    of assuming a developer started a forward by hand (modelled on
    tmi_common.ensure_oauth_stub)."""

    def test_known_names(self):
        self.assertEqual(
            set(deploy._FORWARDS), {"server", "redis", "postgres"},
        )

    def test_unknown_name_exits(self):
        with self.assertRaises(SystemExit):
            deploy.ensure_port_forward("nope")

    def test_healthy_forward_is_not_restarted(self):
        """Idempotence matters: callers invoke this unconditionally, and it must
        not churn a forward that dev-up already established."""
        started = []
        with mock.patch.object(deploy, "_forward_is_healthy", return_value=True), \
             mock.patch.object(deploy, "start_postgres_port_forward",
                               side_effect=lambda: started.append("postgres")):
            deploy.ensure_port_forward("postgres")
        self.assertEqual(started, [], "a healthy forward must not be restarted")

    def test_unhealthy_forward_is_started(self):
        started = []
        with mock.patch.object(deploy, "_forward_is_healthy", return_value=False), \
             mock.patch.object(deploy, "wait_for_port"), \
             mock.patch.object(deploy, "start_postgres_port_forward",
                               side_effect=lambda: started.append("postgres")):
            deploy.ensure_port_forward("postgres")
        self.assertEqual(started, ["postgres"])

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

    def test_healthy_forward_does_not_wait(self):
        """The early return must be cheap -- no polling when nothing started."""
        with mock.patch.object(deploy, "_forward_is_healthy", return_value=True), \
             mock.patch.object(deploy, "wait_for_port") as waited:
            deploy.ensure_port_forward("postgres")
        waited.assert_not_called()

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


class _ActiveContextTestCase(unittest.TestCase):
    """Base class that saves/restores deploy._active_context around each test,
    since it is process-global module state that tests must not leak between
    each other."""

    def setUp(self):
        super().setUp()
        saved = deploy._active_context
        self.addCleanup(setattr, deploy, "_active_context", saved)


class TestActiveContextPinning(_ActiveContextTestCase):
    """#955: kubectl() must always carry an explicit --context resolved from
    the caller's own CLUSTER, never the ambient `kubectl config
    current-context` -- the shared, mutable state that let a concurrent
    devenv run for a DIFFERENT cluster race this one (a `dev-down
    CLUSTER=docker-desktop` deleted a concurrent `dev-up CLUSTER=k3s`'s
    workloads)."""

    def test_kubectl_uses_context_pinned_for_k3s(self):
        deploy.set_active_context("k3s")
        with mock.patch.object(deploy, "run_cmd") as run_cmd:
            deploy.kubectl(["get", "pods"])
        run_cmd.assert_called_once()
        self.assertEqual(run_cmd.call_args.args[0],
                         ["kubectl", "--context", "k3s-example", "get", "pods"])

    def test_kubectl_uses_context_pinned_for_docker_desktop(self):
        deploy.set_active_context("docker-desktop")
        with mock.patch.object(deploy, "run_cmd") as run_cmd:
            deploy.kubectl(["get", "pods"])
        self.assertEqual(run_cmd.call_args.args[0],
                         ["kubectl", "--context", "docker-desktop", "get", "pods"])

    def test_kubectl_fails_loudly_without_a_pinned_context(self):
        """Never silently fall back to the ambient context -- raise instead."""
        deploy._active_context = None
        with self.assertRaises(RuntimeError):
            deploy.kubectl(["get", "pods"])

    def test_set_active_context_rejects_unknown_cluster(self):
        with self.assertRaises(ValueError):
            deploy.set_active_context("kind")

    def test_pin_ambient_context_fails_loudly_with_no_context(self):
        with mock.patch.object(deploy, "current_kube_context", return_value=""), \
             self.assertRaises(RuntimeError):
            deploy.pin_ambient_context()

    def test_pin_ambient_context_pins_whatever_is_current(self):
        with mock.patch.object(deploy, "current_kube_context", return_value="some-ambient-ctx"):
            self.assertEqual(deploy.pin_ambient_context(), "some-ambient-ctx")
        self.assertEqual(deploy._require_active_context(), "some-ambient-ctx")


class TestPidfilePathIsPerContext(_ActiveContextTestCase):
    """#955: each forward's pidfile path is scoped to the active context, so
    docker-desktop and k3s can never share, read, or clobber each other's
    pidfile -- the mechanism that let a docker-desktop `dev-down` tear down
    k3s's live server/redis/postgres port-forwards."""

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

    def test_kinds_differ_within_one_cluster(self):
        deploy.set_active_context("docker-desktop")
        paths = {deploy._pidfile_path(k) for k in ("server", "redis", "postgres")}
        self.assertEqual(len(paths), 3)

    def test_requires_a_pinned_context(self):
        deploy._active_context = None
        with self.assertRaises(RuntimeError):
            deploy._pidfile_path("server")


class TestStopPortForwardIsClusterScoped(_ActiveContextTestCase):
    """#955, second half of the incident: `teardown() -> stop_port_forward()`
    used to kill the server/redis/postgres port-forwards regardless of
    cluster -- a docker-desktop `dev-down` killed k3s's live forwards. Every
    pidfile stop_port_forward() touches, and the legacy-reaper pattern it
    falls back to, must be scoped to the ACTIVE cluster only."""

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

        with mock.patch.object(deploy.subprocess, "Popen", side_effect=popen_side_effect), \
             self.assertRaises(FileNotFoundError):
            deploy.import_image_to_node("tmi-server:dev", "desktop-control-plane")

        saver.stdout.close.assert_called_once()   # pipe read end released
        saver.kill.assert_called_once()           # saver stopped, can't block
        saver.wait.assert_called_once()           # reaped in the finally


class TestReusePreinstalledPlatform(unittest.TestCase):
    @staticmethod
    def _res(rc=0, out=""):
        return mock.Mock(returncode=rc, stdout=out)

    def test_webhook_found_returns_namespace_and_name(self):
        with mock.patch.object(deploy, "kubectl", side_effect=[
                self._res(0, "customresourcedefinition/x"),
                self._res(0, "cert-manager cert-manager-webhook\n")]):
            self.assertEqual(deploy._find_cert_manager_webhook(),
                             ("cert-manager", "cert-manager-webhook"))

    def test_no_crd_means_fresh_cluster(self):
        with mock.patch.object(deploy, "kubectl", return_value=self._res(1)):
            self.assertIsNone(deploy._find_cert_manager_webhook())

    def test_second_selector_is_the_fallback(self):
        calls = []

        def fake(args, **_kw):
            calls.append(args)
            if args[:2] == ["get", "crd"]:
                return self._res(0, "crd")
            return self._res(0, "" if len(calls) == 2 else "cert-manager wh\n")
        with mock.patch.object(deploy, "kubectl", side_effect=fake):
            self.assertEqual(deploy._find_cert_manager_webhook(), ("cert-manager", "wh"))
        self.assertEqual(len(calls), 3)
        self.assertIn(deploy.CERT_MANAGER_WEBHOOK_SELECTORS[1], calls[2])

    def test_selectors_require_helm_management(self):
        for sel in (*deploy.CERT_MANAGER_WEBHOOK_SELECTORS, deploy.RELOADER_SELECTOR):
            self.assertIn("app.kubernetes.io/managed-by=Helm", sel)

    def test_reloader_uses_helm_selector_only(self):
        with mock.patch.object(deploy, "kubectl", return_value=self._res(0, "deployment/r")) as k:
            self.assertTrue(deploy._reloader_exists())
        self.assertIn(deploy.RELOADER_SELECTOR, k.call_args[0][0])
        with mock.patch.object(deploy, "kubectl", return_value=self._res(0, "")):
            self.assertFalse(deploy._reloader_exists())

    def _run_base(self, webhook, reloader):
        with mock.patch.object(deploy, "get_project_root", return_value=_REPO_ROOT), \
             mock.patch.object(deploy, "_find_cert_manager_webhook", return_value=webhook), \
             mock.patch.object(deploy, "_reloader_exists", return_value=reloader), \
             mock.patch.object(deploy, "_apply_with_retry"), \
             mock.patch.object(deploy, "kubectl") as k:
            deploy.apply_platform_base("docker-desktop")
        return [c[0][0] for c in k.call_args_list]

    def test_helm_install_skips_vendored_applies(self):
        cmds = self._run_base(("cert-manager", "cert-manager-webhook"), True)
        flat = [" ".join(c) for c in cmds]
        self.assertFalse(any("cert-manager.yml" in c for c in flat))
        self.assertFalse(any("reloader.yml" in c for c in flat))

    def test_no_helm_install_applies_vendored(self):
        flat = [" ".join(c) for c in self._run_base(None, False)]
        self.assertTrue(any(c.startswith("apply --server-side") and "cert-manager.yml" in c for c in flat))
        self.assertTrue(any("reloader.yml" in c for c in flat))

    def test_webhook_outside_cert_manager_namespace_fails_fast(self):
        with mock.patch.object(deploy, "get_project_root", return_value=_REPO_ROOT), \
             mock.patch.object(deploy, "_find_cert_manager_webhook", return_value=("kube-system", "wh")), \
             mock.patch.object(deploy, "kubectl") as k, \
             self.assertRaises(SystemExit):
            deploy.apply_platform_base("docker-desktop")
        k.assert_not_called()


class TestApplyNats(unittest.TestCase):
    """k3s NATS: JetStream on a longhorn PVC, with a one-time orphan-delete of a
    pre-PVC StatefulSet; docker-desktop keeps the plain emptyDir manifest."""

    def _run(self, cluster_target, get_result=None, pod_result=None):
        calls = []
        if pod_result is None:
            pod_result = self._get(0, "data-nats-0")

        def fake_kubectl(args, **kw):
            calls.append((args, kw))
            if args[:4] == ["-n", deploy.NS, "get", "statefulset"]:
                return get_result
            if args[:4] == ["-n", deploy.NS, "get", "pod"]:
                return pod_result
            return mock.Mock(returncode=0, stdout="", stderr="")

        render = mock.Mock(return_value=mock.Mock(stdout="RENDERED"))
        with mock.patch.object(deploy, "get_project_root", return_value=_REPO_ROOT), \
             mock.patch.object(deploy, "run_cmd", render), \
             mock.patch.object(deploy, "kubectl", side_effect=fake_kubectl):
            deploy.apply_nats(cluster_target)
        return calls, render

    @staticmethod
    def _get(rc=0, out="", err=""):
        return mock.Mock(returncode=rc, stdout=out, stderr=err)

    def test_docker_desktop_applies_base_manifest_only(self):
        calls, render = self._run("docker-desktop")
        render.assert_not_called()
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0][0][:2], ["apply", "-f"])
        self.assertTrue(calls[0][0][2].endswith("deployments/k8s/platform/nats.yml"))

    def _assert_overlay_applied(self, calls, render):
        cmd = render.call_args[0][0]
        self.assertEqual(cmd[:4], ["kubectl", "kustomize", "--load-restrictor", "LoadRestrictionsNone"])
        self.assertTrue(cmd[4].endswith(deploy.K3S_NATS_OVERLAY))
        applies = [c for c in calls if c[0] == ["apply", "-f", "-"]]
        self.assertEqual(len(applies), 1)
        self.assertEqual(applies[0][1]["input_text"], "RENDERED")
        self.assertEqual(calls[-1][0][2:4], ["rollout", "status"])

    def _deletes(self, calls):
        return [c[0] for c in calls if "delete" in c[0]]

    def _pod_deletes(self, calls):
        return [c for c in self._deletes(calls) if "pod" in c]

    def test_k3s_adopted_emptydir_pod_is_deleted_before_rollout_wait(self):
        # Regression: the adopted pre-PVC pod is never rolled by the
        # StatefulSet controller (forbidden in-place update), so rollout hung.
        calls, render = self._run("k3s", self._get(0, ""), pod_result=self._get(0, ""))
        pod_dels = self._pod_deletes(calls)
        self.assertEqual(pod_dels, [["-n", deploy.NS, "delete", "pod", "nats-0", "--ignore-not-found"]])
        del_idx = next(i for i, c in enumerate(calls) if c[0] == pod_dels[0])
        apply_idx = next(i for i, c in enumerate(calls) if c[0] == ["apply", "-f", "-"])
        self.assertLess(apply_idx, del_idx)
        self.assertLess(del_idx, len(calls) - 1)  # rollout status comes after
        self._assert_overlay_applied(calls, render)

    def test_k3s_leftover_pvc_statefulset_with_emptydir_pod_is_repaired(self):
        # A run that died after apply leaves the claim StatefulSet plus the old pod.
        calls, _ = self._run("k3s", self._get(0, "data"), pod_result=self._get(0, ""))
        self.assertEqual(len(self._pod_deletes(calls)), 1)
        self.assertFalse(any("statefulset" in c and "delete" in c for c in self._deletes(calls)))

    def test_k3s_missing_pod_is_left_to_the_statefulset(self):
        calls, _ = self._run("k3s", self._get(0, "data"),
                             pod_result=self._get(1, err='Error from server (NotFound): pods "nats-0" not found'))
        self.assertEqual(self._pod_deletes(calls), [])

    def test_k3s_unreadable_pod_fails_fast_before_rollout_wait(self):
        with self.assertRaises(SystemExit):
            self._run("k3s", self._get(0, "data"), pod_result=self._get(1, err="Unable to connect"))

    def test_k3s_fresh_cluster_applies_without_delete(self):
        calls, render = self._run("k3s", self._get(1, err='Error from server (NotFound): statefulsets.apps "nats" not found'))
        self.assertEqual(self._deletes(calls), [])
        self._assert_overlay_applied(calls, render)

    def test_k3s_pre_pvc_statefulset_is_orphan_deleted_before_apply(self):
        calls, render = self._run("k3s", self._get(0, ""))
        dels = self._deletes(calls)
        self.assertEqual(len(dels), 1)
        self.assertIn("--cascade=orphan", dels[0])
        self.assertIn("--ignore-not-found", dels[0])
        del_idx = next(i for i, c in enumerate(calls) if "delete" in c[0])
        apply_idx = next(i for i, c in enumerate(calls) if c[0] == ["apply", "-f", "-"])
        self.assertLess(del_idx, apply_idx)
        self._assert_overlay_applied(calls, render)

    def test_k3s_second_run_with_pvc_does_not_delete(self):
        calls, render = self._run("k3s", self._get(0, "data"))
        self.assertEqual(self._deletes(calls), [])
        self._assert_overlay_applied(calls, render)

    def test_k3s_unreadable_statefulset_fails_fast(self):
        get = self._get(1, err="Unable to connect to the server")
        render = mock.Mock()
        with mock.patch.object(deploy, "get_project_root", return_value=_REPO_ROOT), \
             mock.patch.object(deploy, "run_cmd", render), \
             mock.patch.object(deploy, "kubectl", return_value=get) as k, \
             self.assertRaises(SystemExit):
            deploy.apply_nats("k3s")
        self.assertEqual(k.call_count, 1)  # the get only: no delete, no apply
        render.assert_not_called()

    def test_unknown_cluster_rejected(self):
        with mock.patch.object(deploy, "kubectl") as k, self.assertRaises(SystemExit):
            deploy.apply_nats("aws")
        k.assert_not_called()

    def test_apply_platform_base_passes_cluster_to_nats(self):
        with mock.patch.object(deploy, "get_project_root", return_value=_REPO_ROOT), \
             mock.patch.object(deploy, "_find_cert_manager_webhook", return_value=("cert-manager", "wh")), \
             mock.patch.object(deploy, "_reloader_exists", return_value=True), \
             mock.patch.object(deploy, "_apply_with_retry"), \
             mock.patch.object(deploy, "kubectl"), \
             mock.patch.object(deploy, "apply_nats") as an:
            deploy.apply_platform_base("k3s")
        an.assert_called_once_with("k3s")

    def test_tracked_overlay_swaps_emptydir_for_longhorn_claim(self):
        overlay = _REPO_ROOT / deploy.K3S_NATS_OVERLAY
        out = deploy.run_cmd(["kubectl", "kustomize", "--load-restrictor", "LoadRestrictionsNone",
                              str(overlay)], capture=True).stdout
        sts = out[out.index("kind: StatefulSet"):]
        sts = sts[:sts.index("\n---")] if "\n---" in sts else sts
        self.assertNotIn("emptyDir", sts)
        self.assertIn("volumeClaimTemplates:", sts)
        self.assertIn("storageClassName: longhorn", sts)
        self.assertIn("storage: 3Gi", sts)
        # The claim must carry the name the container mounts at /data/jetstream.
        self.assertRegex(sts, r"mountPath: /data/jetstream\n\s+name: data")
        self.assertRegex(sts, r"volumeClaimTemplates:\n\s*- metadata:\n\s+name: data\n")


class TestApplyOverlayRegistrySubstitution(unittest.TestCase):
    def test_k3s_placeholder_replaced_before_apply(self):
        rendered = f"image: {deploy.K3S_REGISTRY_PLACEHOLDER}/tmi-server:dev\n"
        run = mock.Mock(return_value=mock.Mock(stdout=rendered))
        with mock.patch.object(deploy, "run_cmd", run), \
             mock.patch.object(deploy, "kubectl") as kc:
            deploy.apply_overlay("postgres", "k3s")
        applied = kc.call_args.kwargs["input_text"]
        self.assertIn("k3s-node.example:30500/tmi-server:dev", applied)
        self.assertNotIn(".invalid", applied)

    def test_placeholder_matches_tracked_overlay(self):
        for rel in ("kustomization.yaml", "patches/extractor-image.yaml", "patches/chunkembed-image.yaml"):
            text = (_DEV_DIR / "k3s" / rel).read_text()
            self.assertIn(deploy.K3S_REGISTRY_PLACEHOLDER, text, rel)


class TestSecretCreatorsRun(unittest.TestCase):
    """Smoke-run each secret creator with kubectl mocked, so an undefined name
    (F821) or bad signature in the function body fails a test."""

    def _run(self, fn, env=None):
        with mock.patch.object(deploy, "kubectl", return_value=mock.Mock(stdout="kind: Secret\n")) as kc, \
             mock.patch.dict(os.environ, env or {}):
            fn()
        self.assertEqual(kc.call_args.args[0], ["apply", "-f", "-"])
        self.assertTrue(kc.call_args.kwargs["input_text"])

    def test_embedding(self):
        self._run(deploy.create_embedding_secret)

    def test_oauth_providers(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / ".local").mkdir()
            (Path(d) / ".local" / "oauth-providers.env").write_text("FOO=bar\n")
            with mock.patch.object(deploy, "get_project_root", return_value=Path(d)):
                self._run(deploy.create_oauth_providers_secret)

    def test_oracle_wallet(self):
        import tempfile
        with tempfile.NamedTemporaryFile(suffix=".zip") as f:
            self._run(deploy.create_oracle_wallet_secret, {"TMI_ORACLE_WALLET_ZIP": f.name})

    def test_oracle_db(self):
        self._run(deploy.create_oracle_db_secret,
                  {"TMI_DATABASE_URL": "oracle://x", "ORACLE_PASSWORD": "p"})




def _secret_values(tc, manifest, name):
    """Decode the Opaque Secret manifest a helper piped to kubectl on stdin."""
    doc = json.loads(manifest)
    tc.assertEqual((doc["kind"], doc["type"]), ("Secret", "Opaque"))
    tc.assertEqual(doc["metadata"], {"name": name, "namespace": deploy.NS})
    return {k: base64.b64decode(v).decode() for k, v in doc["data"].items()}


class TestCreateEmbeddingSecret(unittest.TestCase):
    """create_embedding_secret never clobbers an existing key (#1243) and never
    puts a key value on kubectl's argv."""

    def _run(self, *, exists, env_key=None):
        calls = []
        file_values = {}

        def fake_kubectl(args, **kw):
            calls.append(list(args))
            if args[:3] == ["-n", deploy.NS, "get"]:
                return mock.Mock(returncode=0 if exists else 1, stdout="")
            if args == ["apply", "-f", "-"]:
                file_values.update(_secret_values(self, kw["input_text"], "tmi-embedding"))
            return mock.Mock(returncode=0, stdout="")

        env = {"TMI_EMBEDDING_API_KEY": env_key} if env_key is not None else {}
        with mock.patch.object(deploy, "kubectl", side_effect=fake_kubectl), \
             mock.patch.dict(os.environ, env, clear=False), \
             mock.patch.object(deploy, "log_info"), mock.patch.object(deploy, "log_warn"):
            if env_key is None:
                os.environ.pop("TMI_EMBEDDING_API_KEY", None)
            deploy.create_embedding_secret()
        return calls, file_values

    def test_unset_and_existing_secret_is_left_unchanged(self):
        calls, values = self._run(exists=True)
        self.assertEqual(calls, [["-n", deploy.NS, "get", "secret", "tmi-embedding"]])
        self.assertEqual(values, {})

    def test_unset_and_missing_secret_gets_placeholder(self):
        calls, values = self._run(exists=False)
        self.assertEqual(values, {"api-key": deploy.EMBEDDING_PLACEHOLDER_KEY})
        self.assertEqual(calls[-1], ["apply", "-f", "-"])

    def test_set_key_overwrites_existing_secret_via_stdin(self):
        secret = "sk-real-" + "x" * 40
        calls, values = self._run(exists=True, env_key=secret)
        self.assertEqual(values, {"api-key": secret})
        self.assertFalse(any(secret in a for c in calls for a in c), "key leaked to argv")
        self.assertEqual(calls[-1], ["apply", "-f", "-"])

    def test_empty_key_is_treated_as_unset(self):
        _calls, values = self._run(exists=True, env_key="")
        self.assertEqual(values, {})


class TestOracleDbSecretArgv(unittest.TestCase):
    def test_values_never_on_argv(self):
        seen = []

        def fake_kubectl(args, **kw):
            seen.append(list(args))
            return mock.Mock(returncode=0, stdout="kind: Secret\n")

        env = {"TMI_DATABASE_URL": "oracle://u@h/svc", "ORACLE_PASSWORD": "s3cret-pw"}
        with mock.patch.object(deploy, "kubectl", side_effect=fake_kubectl), \
             mock.patch.dict(os.environ, env), mock.patch.object(deploy, "log_success"):
            deploy.create_oracle_db_secret()
        flat = [a for c in seen for a in c]
        self.assertFalse(any("s3cret-pw" in a or "oracle://" in a for a in flat))
        self.assertEqual(seen, [["apply", "-f", "-"]])

    def test_values_go_through_stdin_manifest(self):
        kc = mock.Mock(return_value=mock.Mock(returncode=0, stdout=""))
        env = {"TMI_DATABASE_URL": "oracle://u@h/svc", "ORACLE_PASSWORD": "s3cret-pw"}
        with mock.patch.object(deploy, "kubectl", kc), \
             mock.patch.dict(os.environ, env), mock.patch.object(deploy, "log_success"):
            deploy.create_oracle_db_secret()
        values = _secret_values(self, kc.call_args.kwargs["input_text"], "tmi-oracle-db")
        self.assertEqual(values["oracle-password"], "s3cret-pw")


class TestEnsureRedisPasswordSecret(unittest.TestCase):
    def _run(self, exists):
        calls = []

        def fake_kubectl(args, **kw):
            calls.append((list(args), kw))
            if args[:3] == ["-n", deploy.NS, "get"]:
                return mock.Mock(returncode=0 if exists else 1, stdout="")
            return mock.Mock(returncode=0, stdout="")

        with mock.patch.object(deploy, "kubectl", side_effect=fake_kubectl), \
             mock.patch.object(deploy, "log_success"):
            deploy.ensure_redis_password_secret()
        return calls

    def test_existing_secret_untouched(self):
        self.assertEqual(len(self._run(exists=True)), 1)

    def test_missing_secret_created_from_stdin(self):
        calls = self._run(exists=False)
        args, kw = calls[-1]
        self.assertEqual(args, ["create", "-f", "-"])
        pw = _secret_values(self, kw["input_text"], "tmi-secrets")["TMI_REDIS_PASSWORD"]
        self.assertGreaterEqual(len(pw), 32)
        self.assertFalse(any(pw in a for a, _ in calls for a in a), "password leaked to argv")


class TestSecretManifest(unittest.TestCase):
    def test_round_trips_values(self):
        m = deploy._secret_manifest("s", {"a": "x\ny", "b": ""})
        self.assertEqual(_secret_values(self, m, "s"), {"a": "x\ny", "b": ""})


class TestSeedTmiSecretKeys(unittest.TestCase):
    """seed_tmi_secret_keys merges missing keys and never overwrites (#965)."""

    CONFIG = 'database:\n  url: "postgres://u:pw@localhost:5432/d?sslmode=disable"\n'

    def _run(self, present, db="postgres"):
        patches = []

        def fake_kubectl(args, **kw):
            if args[:3] == ["-n", deploy.NS, "get"]:
                key = args[-1].split(".data.")[1].rstrip("}")
                return mock.Mock(returncode=0, stdout="x" if key in present else "")
            if "patch" in args:
                self.assertIn("--patch-file=/dev/stdin", args)
                patches.append(json.loads(kw["input_text"])["data"])
            return mock.Mock(returncode=0, stdout="")

        cfg = mock.Mock(read_text=lambda: self.CONFIG)
        with mock.patch.object(deploy, "kubectl", side_effect=fake_kubectl), \
             mock.patch.object(deploy, "get_project_root", return_value=mock.MagicMock(__truediv__=lambda s, o: cfg)), \
             mock.patch.object(deploy, "log_success"):
            deploy.seed_tmi_secret_keys("docker-desktop", db)
        return patches

    def test_adds_all_when_absent(self):
        (data,) = self._run(set())
        self.assertEqual(set(data), {"TMI_SECRET_SETTINGS_ENCRYPTION_KEY",
                                     "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID", "TMI_DATABASE_URL"})
        self.assertEqual(base64.b64decode(data["TMI_DATABASE_URL"]).decode(),
                         "postgres://u:pw@postgres:5432/d?sslmode=disable")
        self.assertEqual(base64.b64decode(data["TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"]), b"1")

    def test_never_overwrites_existing(self):
        (data,) = self._run({"TMI_SECRET_SETTINGS_ENCRYPTION_KEY", "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"})
        self.assertEqual(set(data), {"TMI_DATABASE_URL"})

    def test_no_patch_when_all_present(self):
        self.assertEqual(self._run({"TMI_SECRET_SETTINGS_ENCRYPTION_KEY",
                                    "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID", "TMI_DATABASE_URL"}), [])

    def test_oracle_skips_database_url(self):
        (data,) = self._run(set(), db="oracle")
        self.assertNotIn("TMI_DATABASE_URL", data)


if __name__ == "__main__":
    unittest.main()
