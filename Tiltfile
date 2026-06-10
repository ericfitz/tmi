# Tilt fast inner loop for the TMI server ONLY.
#
# Prereq: `make start-dev` has already deployed the full dev environment
# (cluster + infra + workers + a prod-shaped tmi-server). `tilt up` takes over
# deploy/tmi-server, swapping it for a live-updatable image: edits to the Go
# sources recompile the binary on the host and sync it into the running
# container, restarting the process in place (~seconds, no image rebuild/roll).
#
# `tilt down` removes Tilt's tmi-server; `make tilt-down` then re-applies the
# canonical server.yml to restore the prod-shaped server.

load('ext://restart_process', 'docker_build_with_restart')

# Push the devloop image to the same local registry the cluster pulls from.
default_registry('localhost:5000')

# 1) Compile the dev binary on the host (fast, incremental). Watching the Go
#    source trees triggers a recompile on save.
local_resource(
    'server-compile',
    cmd='go build -tags=dev -o bin/tmiserver ./cmd/server',
    deps=['cmd/server', 'api', 'auth', 'internal', 'pkg'],
)

# 2) One-COPY image on the prod static base; live-update syncs just the binary
#    and the restart_process wrapper re-execs it in place.
#
#    IMAGE NAME NOTE: The name below ('localhost:5000/tmi-server', no tag) is
#    matched against server.yml's image ref ('localhost:5000/tmi-server:dev').
#    Tilt matches by name and ignores the tag when substituting into deployed
#    objects; `default_registry` and the build name together handle tagging.
#    If Tilt fails to substitute the image (e.g. "no images matched"), use the
#    exact tagged ref as the fallback:
#        docker_build_with_restart('localhost:5000/tmi-server:dev', ...)
#
#    RESTART_PROCESS FALLBACK: restart_process works by injecting a small static
#    `tilt-restart-wrapper` binary, which MAY work on cgr.dev/chainguard/static
#    (distroless, no shell) because the wrapper is itself a static binary. If
#    Task 4 acceptance testing reveals the wrapper cannot run on the Chainguard
#    base, replace docker_build_with_restart with the following plain docker_build:
#
#        docker_build(
#            'localhost:5000/tmi-server',
#            '.',
#            dockerfile='Dockerfile.server-devloop',
#            only=['./bin/tmiserver'],
#            live_update=[sync('./bin/tmiserver', '/tmiserver')],
#        )
#
#    This falls back to a rolling image rebuild on binary change (slower than
#    in-place restart, but still faster than a full image rebuild + push cycle).
docker_build_with_restart(
    'localhost:5000/tmi-server',
    '.',
    dockerfile='Dockerfile.server-devloop',
    entrypoint=['/tmiserver', '--config=/etc/tmi/config.yml'],
    only=['./bin/tmiserver'],
    live_update=[sync('./bin/tmiserver', '/tmiserver')],
)

# 3) Deploy ONLY the server (the rest of the env is owned by start-dev). Tilt
#    matches the image ref in server.yml and substitutes the freshly built one.
k8s_yaml('deployments/k8s/dev/server.yml')
k8s_resource('tmi-server', port_forwards='8080:8080', resource_deps=['server-compile'])
