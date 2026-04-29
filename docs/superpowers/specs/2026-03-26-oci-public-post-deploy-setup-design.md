# OCI Public Post-Deploy Setup Design

**Date:** 2026-03-26
**Status:** Draft

## Overview

This spec covers two related changes needed to complete the public OCI deployment:

1. **Terraform changes** — Replace per-service OCI Load Balancers with a single LB managed by the OCI Native Ingress Controller, enabling host-based routing and staying within the Always Free tier (one 10 Mbps flexible LB).

2. **Setup script** — A Python CLI script (`scripts/setup-oci-public.py`) that completes deployment-specific configuration: DNS records, TLS certificates, OAuth providers, CORS, and webhook registration for tmi-tf-wh. The script and its logic are gitignored-safe (no secrets in code); secrets come from a local `.env` file.

## Goals

At the end of the setup script, the deployment has:

1. HTTPS-only load balancing with valid public certificates for `api.oci.tmi.dev` and `app.oci.tmi.dev` (port 80 closed).
2. All runtime configuration deployed: Google OAuth provider, CORS allowed origins, HTTP webhook allowance.
3. tmi-tf-wh registered as a webhook within the TMI server, with its own client credentials for API callbacks.

## Non-Goals

- Modifying `make deploy-oci` or the deploy script itself.
- Building or pushing container images.
- Creating the OCI DNS zone (must pre-exist).
- Creating the Google OAuth client in Google Cloud Console (user provides credentials).
- Performing the initial admin OAuth login (user provides pre-existing client credentials).
- Configuring tmi-ux environment variables (baked into the container image at build time via `environment.oci.ts`).

---

## Part 1: Terraform — OCI Native Ingress Controller

### Motivation

The current architecture creates a separate OCI Flexible Load Balancer for each Kubernetes `LoadBalancer` Service (tmi-api, tmi-ux). OCI Always Free tier includes only one 10 Mbps flexible LB, so two services means paying for the second. Switching to a single LB with host-based routing eliminates this cost.

### Approach

Use the **OCI Native Ingress Controller** — an Oracle-maintained, open-source controller published at `container-registry.oracle.com/oci/oci-native-ingress-controller`. It runs as a pod, watches Kubernetes `Ingress` resources, and configures the OCI Load Balancer directly via OCI APIs. No third-party images required.

### Architecture

```
DNS: api.oci.tmi.dev ──┐
                        ├─→ Single OCI LB (10 Mbps, free tier)
DNS: app.oci.tmi.dev ──┘           │
                                   ↓
                     OCI Native Ingress Controller pod
                        (watches Ingress resources)
                                   │
                     ┌─────────────┼─────────────┐
                     ↓                           ↓
             tmi-api (ClusterIP)        tmi-ux (ClusterIP)

             tmi-tf-wh (ClusterIP, internal only)
```

### Terraform Resource Changes

**New resources in the kubernetes/oci module:**

| Resource | Purpose |
|----------|---------|
| `oci_containerengine_addon` | Enables OCI Native Ingress Controller on the OKE cluster |
| `kubernetes_ingress_class_v1` | Registers the `oci-native-ic` ingress class |
| `kubernetes_ingress_v1` | Host-based routing: `api_hostname` → tmi-api, `ux_hostname` → tmi-ux |

**Modified resources:**

| Resource | Change |
|----------|--------|
| `kubernetes_service_v1.tmi_api` | Type `LoadBalancer` → `ClusterIP`; remove all OCI LB annotations |
| `kubernetes_service_v1.tmi_ux` | Type `LoadBalancer` → `ClusterIP`; remove all OCI LB annotations |
| `kubernetes_service_v1.tmi_tf_wh` | Always `ClusterIP` (remove conditional `LoadBalancer` for public) |

**Removed concerns:**

Per-service LB annotations for SSL, shape, bandwidth, and NSG move to the Ingress resource and ingress controller configuration.

**New variables:**

| Variable | Type | Default | Purpose |
|----------|------|---------|---------|
| `api_hostname` | string | `null` | Hostname for API ingress rule (e.g., `api.oci.tmi.dev`) |
| `ux_hostname` | string | `null` | Hostname for UX ingress rule (e.g., `app.oci.tmi.dev`) |
| `ingress_lb_subnet_id` | string | — | Public subnet OCID for the ingress controller's LB (reuse existing public subnet) |

**TLS handling:**

- The `Ingress` resource references a K8s TLS secret named `tmi-tls`.
- The setup script (Part 2) creates this secret after obtaining the wildcard cert from certmgr.
- Until the cert is provisioned, the ingress operates on HTTP only.
- The certmgr function is used for ongoing renewal; the setup script handles initial issuance and K8s secret creation.

**Ingress controller namespace and NSG:**

The OCI Native Ingress Controller add-on runs in its own namespace (typically `native-ingress-controller-system`, managed by OKE). The LB it creates needs inbound access on port 443. The ingress controller add-on configuration includes the public subnet OCID and NSG OCIDs so it can provision the LB in the correct network. These are passed via the add-on configuration parameters.

**Certmgr and the ingress LB:**

The certmgr function needs the LB OCID to update certificates directly. Since the ingress controller creates the LB (not terraform), the LB OCID must be discovered post-deploy. Two options:
- The setup script discovers the LB OCID (via `oci lb load-balancer list` filtered by compartment and tags/name) and updates the certmgr function's configuration.
- The setup script handles cert issuance and K8s secret creation entirely, bypassing certmgr's LB-update feature. Certmgr is only used for ACME + Vault operations; the script copies the cert from Vault into the K8s TLS secret.

The second option is simpler and avoids coupling certmgr to the ingress controller's LB. The ingress controller reads TLS from the K8s secret, so updating the secret is sufficient. Certmgr renewal would follow the same pattern: function renews cert in Vault, a cron job or scheduled script copies from Vault to K8s secret.

**Impact on deploy-oci.sh:**

None. The two-phase deploy (Phase 1: infra + OKE + addon, Phase 2: K8s resources including Ingress) continues to work as-is.

---

## Part 2: Setup Script

### File Structure

```
scripts/setup-oci-public.py          # Main script (committed, no secrets)
scripts/setup-oci-public.env         # Secrets file (gitignored)
scripts/setup-oci-public.env.example # Template (committed)
```

A `.gitignore` entry is added for `scripts/setup-oci-public.env`.

### .env File Template

```bash
# OCI Configuration
OCI_PROFILE=tmi
OCI_COMPARTMENT_ID=ocid1.compartment...
OCI_DNS_ZONE_ID=ocid1.dns-zone...
OCI_REGION=us-ashburn-1

# Domain
DOMAIN=oci.tmi.dev
API_HOSTNAME=api.oci.tmi.dev
UX_HOSTNAME=app.oci.tmi.dev

# ACME / Let's Encrypt
ACME_EMAIL=admin@tmi.dev
ACME_DIRECTORY=production

# Google OAuth
GOOGLE_CLIENT_ID=<your-google-client-id>
GOOGLE_CLIENT_SECRET=<your-google-client-secret>

# TMI Admin Bootstrap (client credentials from a prior manual login as admin)
TMI_CLIENT_ID=tmi_cc_...
TMI_CLIENT_SECRET=...

# Kubernetes
KUBE_CONTEXT=
TMI_NAMESPACE=tmi
```

### Python Dependencies (inline uv TOML)

```python
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "click>=8.0",
#     "requests>=2.31",
#     "python-dotenv>=1.0",
# ]
# ///
```

External tools called via `subprocess.run`: `oci`, `kubectl`, `dig`.

### CLI Interface

```bash
# Run all phases in order
uv run scripts/setup-oci-public.py all

# Run individual phases
uv run scripts/setup-oci-public.py verify
uv run scripts/setup-oci-public.py dns
uv run scripts/setup-oci-public.py certs
uv run scripts/setup-oci-public.py configure
uv run scripts/setup-oci-public.py webhook

# Options
uv run scripts/setup-oci-public.py all --dry-run
uv run scripts/setup-oci-public.py all --env-file scripts/my-other.env
```

### Phase 0: `verify`

Checks prerequisites before doing anything:

- `oci` CLI installed, profile works (`oci iam region list --profile <profile>`)
- `kubectl` connected to correct cluster, namespace exists
- tmi-api pods running and healthy
- tmi-ux pods running (if deployment exists)
- tmi-tf-wh pods running (if deployment exists)
- Ingress controller pod running
- Ingress LB has a public IP assigned
- OCI DNS zone accessible

Fails fast with clear, actionable error messages.

### Phase 1: `dns`

1. Get the ingress LB's public IP from `kubectl get svc -n <ingress-ns>` (the ingress controller's LoadBalancer service).
2. Check if A records already exist for `api.oci.tmi.dev` and `app.oci.tmi.dev` via OCI DNS API.
3. If missing or pointing to wrong IP, create/update A records via `oci dns record zone patch-zone-records`.
4. Poll DNS resolution with `dig` (every 15 seconds, up to 5 minutes) until both hostnames resolve to the LB IP.

**Idempotency:** Skips if records already exist and point to the correct IP.

### Phase 2: `certs`

1. Check if K8s TLS secret `tmi-tls` exists with a valid cert (>7 days until expiry).
2. If valid, skip.
3. Otherwise, invoke the certmgr OCI Function via `oci fn function invoke` to issue/renew a wildcard cert for `*.oci.tmi.dev`.
4. If certmgr function doesn't exist (cert automation not enabled in terraform), fail with message: "Enable `enable_certificate_automation = true` in terraform.tfvars and re-run `make deploy-oci`".
5. Retrieve cert and private key from OCI Vault via `oci vault secret get-secret-bundle`.
6. Create/update K8s TLS secret `tmi-tls` via `kubectl create secret tls --dry-run=client -o yaml | kubectl apply -f -`.
7. Verify HTTPS is working: `curl -s https://api.oci.tmi.dev/` returns a response.

**Idempotency:** Skips if valid cert exists. Re-running after expiry triggers renewal.

### Phase 3: `configure`

1. Exchange admin client credentials for a JWT: `POST <tmi-api-url>/oauth2/token` with `grant_type=client_credentials`. The URL is `https://api.oci.tmi.dev` if certs are already configured, or discovered via `kubectl port-forward` if HTTPS is not yet available (the `all` command runs `certs` before `configure`, so HTTPS is normally available).
2. **CORS + HTTP webhooks:** Patch the tmi-api ConfigMap to add/update:
   - `TMI_CORS_ALLOWED_ORIGINS=https://app.oci.tmi.dev`
   - `TMI_WEBHOOK_ALLOW_HTTP_TARGETS=true`
3. If ConfigMap already has correct values, skip patch and restart.
4. Otherwise, `kubectl rollout restart deployment/tmi-api` and wait for pods ready.
5. **Google OAuth:** Configure via `PUT /admin/settings/{key}` for each:
   - `auth.oauth.providers.google.enabled` = `true`
   - `auth.oauth.providers.google.client_id` = `<from env>`
   - `auth.oauth.providers.google.client_secret` = `<from env>`
   - `auth.oauth.providers.google.scopes` = `["openid", "email", "profile"]`
6. Verify Google OAuth appears in the provider list.

**Idempotency:** Checks current ConfigMap values and setting values before writing. Skips if already correct.

### Phase 4: `webhook`

1. Authenticate as admin (client credentials → JWT, same as Phase 3).
2. List existing webhook subscriptions (`GET /webhooks/subscriptions`), check if one named `tmi-tf-wh` exists.
3. If active subscription exists with correct URL and events, skip.
4. If not, create: `POST /webhooks/subscriptions` with:
   - `name`: `"tmi-tf-wh"`
   - `url`: `"http://tmi-tf-wh:8080"`
   - `events`: `["repository.created", "repository.updated", "addon.invoked"]`
5. Poll subscription status (every 15 seconds, up to 3 minutes) until `status` = `active` (challenge verification complete).
6. If verification fails after timeout, fail with diagnostic: check tmi-tf-wh pod logs, verify service reachability.
7. Create client credentials for tmi-tf-wh: `POST /me/client_credentials` with name `"tmi-tf-wh-service"`.
8. Check if tmi-tf-wh deployment already has client credentials configured. If so, skip.
9. Patch tmi-tf-wh ConfigMap/Secret with the new `TMI_CLIENT_ID` and `TMI_CLIENT_SECRET`.
10. `kubectl rollout restart deployment/tmi-tf-wh`, wait for ready.
11. Verify tmi-tf-wh health check passes.

**Idempotency:** Skips subscription creation if already exists and active. Client credential creation is not idempotent (creates a new one each time), so the script checks if creds are already configured before creating.

---

## Error Handling

### Authentication failures
- Invalid client credentials: fail immediately with instructions on how to obtain them (manual OAuth login, then `POST /me/client_credentials`).
- Token expiry mid-run: refresh before each phase when running `all`.

### Certmgr failures
- Function not deployed: clear error pointing to terraform configuration.
- ACME rate limits: detect and suggest using `staging` directory first.
- Vault access errors: check IAM policies.

### DNS propagation
- Poll resolution up to 5 minutes. If DNS doesn't propagate, fail with suggestion to re-run `dns` then `certs`.
- The `certs` phase requires DNS to resolve (certmgr uses DNS-01 challenges).

### Webhook challenge
- Poll subscription status up to 3 minutes (challenge worker runs every 30s, 3 max attempts).
- On failure: diagnostic message to check tmi-tf-wh pod logs and service reachability.

### ConfigMap patching
- Uses `kubectl get configmap -o json`, merges new keys in Python, then `kubectl apply`.
- Preserves all existing keys — only adds/updates specific ones.

### Partial completion
- Phases are independently runnable. Failure in Phase 2 doesn't prevent re-running Phase 3.
- `all` command stops on first phase failure (fail-fast).
- Re-running completed phases is safe (idempotent no-ops).

---

## External Tool Requirements

| Tool | Phases | Purpose |
|------|--------|---------|
| `oci` | verify, dns, certs | OCI CLI (API key auth, profile-based) |
| `kubectl` | verify, certs, configure, webhook | K8s operations |
| `dig` | dns | DNS propagation verification |

The `verify` phase checks all required tools are installed and accessible.

### OCI CLI Auth

Uses API key auth with the profile from `OCI_PROFILE` (no `--auth security_token`). All `oci` commands include `--profile $OCI_PROFILE`.

### Kubernetes Auth

Uses the context from `KUBE_CONTEXT` or the current context. All `kubectl` commands include explicit `--context` and `--namespace` flags.

---

## Always Free Tier Constraints

| Resource | Limit | Usage |
|----------|-------|-------|
| OCI Flexible LB | 1 × 10 Mbps | Single LB via ingress controller |
| A1 Compute | 4 OCPU / 24 GB | 2 OCPU / 12 GB node + ingress controller pod (~50-100 MB) |
| OCI Functions | 400K seconds/month | Certmgr invocation: negligible |
| OCI Vault | 20 keys | 3 secrets (ACME key, cert, private key) |
| OCI DNS | Free hosted zones | 1 zone, 2 A records |

---

## Execution Order

For a fresh deployment:

1. `make deploy-oci` (provisions all infrastructure including ingress controller)
2. Manual: perform initial OAuth login as admin user (login_hint=charlie), create client credentials via `POST /me/client_credentials`
3. Fill in `scripts/setup-oci-public.env` with credentials and configuration
4. `uv run scripts/setup-oci-public.py all`

For subsequent runs (e.g., cert renewal, config changes):

- `uv run scripts/setup-oci-public.py certs` (renew cert)
- `uv run scripts/setup-oci-public.py configure` (update OAuth or CORS settings)
