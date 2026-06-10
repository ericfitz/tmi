# Tilt fast inner loop for the TMI server ONLY.
#
# Prereq: `make start-dev` has already deployed the full dev environment
# (cluster + infra + workers + a prod-shaped tmi-server). `tilt up` takes over
# deploy/tmi-server, swapping it for a fast-rebuild image: editing the Go
# sources recompiles the binary on the host, Tilt rebuilds a one-COPY image on
# the prod static base and rolls the server in ~seconds — vs. minutes for
# `make restart-dev`, which rebuilds all four images.
#
# `tilt down` removes Tilt's tmi-server; `make tilt-down` then re-applies the
# canonical server.yml to restore the prod-shaped server.
#
# SCOPE: Postgres path only (by design — see the #442 design spec). This loop
# always builds/restores the Postgres server.yml; the Oracle CGO image
# (DB=oracle) is out of fast-loop scope. If you brought the env up with
# DB=oracle, `make tilt-down` will restore the Postgres server — re-run
# `make start-dev DB=oracle` to get the Oracle server back.
#
# WHY NOT in-place live_update + restart_process (the sub-second ideal)?
#   The prod runtime base is cgr.dev/chainguard/static (distroless: no shell,
#   no coreutils). The restart_process extension builds a helper layer that runs
#   `RUN ["touch", "/tmp/.restart-proc"]` ON TOP of our base image — which fails
#   on chainguard/static with `exec: "touch": executable file not found`. An
#   in-place sync also can't restart PID 1 in a shell-less image. Rather than
#   abandon the prod base (shape fidelity matters), we keep it and accept a fast
#   one-COPY rebuild + rollout. (Verified during issue #442 Plan 3 acceptance;
#   `docker_build_with_restart` was tried first and failed exactly here.)

# Push the devloop image to the same local registry the cluster pulls from.
default_registry('localhost:5000')

# 1) Cross-compile the dev binary on the host for the LINUX container (fast,
#    incremental). Watching the Go source trees triggers a recompile on save.
#    GOOS=linux is REQUIRED: the host may be macOS (darwin); a native build
#    produces a Mach-O binary that fails in the container with "exec format
#    error". GOARCH defaults to the host arch, which matches the local kind/k3s
#    node (same machine). CGO_ENABLED=0 mirrors the prod Dockerfile.server build
#    and keeps the binary static for the distroless base.
local_resource(
    'server-compile',
    cmd='CGO_ENABLED=0 GOOS=linux go build -tags=dev -o bin/tmiserver ./cmd/server',
    deps=['cmd/server', 'api', 'auth', 'internal', 'pkg'],
)

# 2) One-COPY image on the prod static base. When the host-compiled binary
#    changes, Tilt rebuilds this trivial image (~1-2s) and rolls deploy/tmi-server.
#    The container command is `/tmiserver --config=/etc/tmi/config.yml`: the
#    image's ENTRYPOINT (`/tmiserver`) plus server.yml's args (`--config=...`).
#
#    IMAGE NAME NOTE: the name below ('localhost:5000/tmi-server', no tag) is
#    matched against server.yml's image ref ('localhost:5000/tmi-server:dev').
#    Tilt matches by name and ignores the tag when substituting into deployed
#    objects. If Tilt ever fails to substitute ("no images matched"), use the
#    exact tagged ref: docker_build('localhost:5000/tmi-server:dev', ...).
docker_build(
    'localhost:5000/tmi-server',
    '.',
    dockerfile='Dockerfile.server-devloop',
    only=['./bin/tmiserver'],
)

# 3) Deploy ONLY the server (the rest of the env is owned by start-dev). Tilt
#    matches the image ref in server.yml and substitutes the freshly built one.
#    A binary change auto-triggers rebuild + rollout (TRIGGER_MODE_AUTO default).
k8s_yaml('deployments/k8s/dev/server.yml')
k8s_resource('tmi-server', port_forwards='8080:8080', resource_deps=['server-compile'])
