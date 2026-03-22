# OCI Clean Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy tmi, tmi-ux, and tmi-tf-wh to a clean OCI public environment with all required infrastructure, container images, and configuration.

**Architecture:** Terraform creates all OCI infrastructure (VCN, OKE, ADB, Vault, OCIR repos) and K8s resources in a single apply. Container images are built and pushed after infrastructure is up. K8s pods auto-recover once images are available.

**Tech Stack:** Terraform (OCI provider), Docker buildx (ARM64), Bash, OCI CLI, Kubernetes

**Spec:** `docs/superpowers/specs/2026-03-21-oci-clean-deploy-design.md`

**Note:** The deployment runbook (spec deliverable #4) is documented in the spec itself (Section "4. Deployment Runbook"). It is a manual operational procedure, not a code artifact to implement.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `terraform/environments/oci-public/terraform.tfvars` | OCI-specific deployment values |
| Modify | `terraform/environments/oci-public/main.tf` | Add tmi-ux OCIR repository resource |
| Modify | `terraform/environments/oci-private/main.tf` | Add tmi-ux OCIR repository resource (private) |
| Create | `/Users/efitz/Projects/tmi-tf-wh/scripts/push-oci.sh` | Build and push tmi-tf-wh container to OCIR |

---

### Task 1: Add tmi-ux OCIR Repository to Terraform

**Files:**
- Modify: `terraform/environments/oci-public/main.tf:127` (after redis repo resource)
- Modify: `terraform/environments/oci-private/main.tf:141` (after redis repo resource)

- [ ] **Step 1: Add tmi-ux OCIR repo to oci-public main.tf**

Insert after the `oci_artifacts_container_repository.redis` resource (line 126) and before `oci_artifacts_container_repository.tmi_tf_wh` (line 128):

```hcl
resource "oci_artifacts_container_repository" "tmi_ux" {
  count          = var.tmi_ux_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-ux"
  is_public      = true
}
```

- [ ] **Step 2: Add tmi-ux OCIR repo to oci-private main.tf**

Insert after the `oci_artifacts_container_repository.redis` resource (line 140) and before `oci_artifacts_container_repository.tmi_tf_wh` (line 142):

```hcl
resource "oci_artifacts_container_repository" "tmi_ux" {
  count          = var.tmi_ux_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-ux"
  is_public      = false
}
```

- [ ] **Step 3: Validate Terraform configuration**

Run:
```bash
cd /Users/efitz/Projects/tmi
make tf-validate
```

Expected: `Success! The configuration is valid.`

If `make tf-validate` doesn't exist, run directly:
```bash
cd terraform/environments/oci-public && terraform validate
```

- [ ] **Step 4: Format Terraform files**

Run:
```bash
cd /Users/efitz/Projects/tmi
terraform fmt terraform/environments/oci-public/main.tf
terraform fmt terraform/environments/oci-private/main.tf
```

Expected: Files formatted (or no changes if already formatted).

- [ ] **Step 5: Commit**

```bash
cd /Users/efitz/Projects/tmi
git add terraform/environments/oci-public/main.tf terraform/environments/oci-private/main.tf
git commit -m "$(cat <<'EOF'
feat(terraform): add tmi-ux OCIR container repository resource

Add oci_artifacts_container_repository for tmi-ux in both oci-public
(public repo) and oci-private (private repo) environments. Conditional
on tmi_ux_enabled variable, matching the pattern used for tmi-tf-wh.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Create terraform.tfvars

**Files:**
- Create: `terraform/environments/oci-public/terraform.tfvars`
- Reference: `terraform/environments/oci-public/terraform.tfvars.example`
- Reference: `terraform/environments/oci-public/variables.tf`

- [ ] **Step 1: Create terraform.tfvars with all deployment values**

Create `terraform/environments/oci-public/terraform.tfvars`:

```hcl
# TMI OCI Public Deployment Configuration
# Generated: 2026-03-21

# ---------------------------------------------------------------------------
# OCI Identity
# ---------------------------------------------------------------------------
tenancy_ocid       = "ocid1.tenancy.oc1..aaaaaaaaxlnh6tajzzylq7mm7mwu2wtf62ac2gvbrswbqlftnjwpmkq4pyka"
compartment_id     = "ocid1.compartment.oc1..aaaaaaaagclwz3dtsb5z3kcqxbxyajrpq3zhk6usz5vwuhey6naxbm5aikqq"
region             = "us-ashburn-1"
oci_config_profile = "tmi"

# ---------------------------------------------------------------------------
# OKE Node Image (ARM64, Oracle Linux 8.10, K8s v1.34.2)
# ---------------------------------------------------------------------------
node_image_id = "ocid1.image.oc1.iad.aaaaaaaamiw5rdac6ixhhtucfgs4xv25wioklldmhe6tcd6hlot6uba7yyga"

# ---------------------------------------------------------------------------
# Container Images (OCIR)
# ---------------------------------------------------------------------------
tmi_image_url   = "iad.ocir.io/idqeh6gjpmoe/tmi/tmi:latest"
redis_image_url = "iad.ocir.io/idqeh6gjpmoe/tmi/tmi-redis:latest"

# ---------------------------------------------------------------------------
# TMI-UX Frontend
# ---------------------------------------------------------------------------
tmi_ux_enabled   = true
tmi_ux_image_url = "iad.ocir.io/idqeh6gjpmoe/tmi/tmi-ux:latest"

# ---------------------------------------------------------------------------
# TMI-TF-WH Webhook Analyzer
# ---------------------------------------------------------------------------
tmi_tf_wh_enabled   = true
tmi_tf_wh_image_url = "iad.ocir.io/idqeh6gjpmoe/tmi/tmi-tf-wh:latest"

# ---------------------------------------------------------------------------
# Deployer Environment Variables
# ---------------------------------------------------------------------------
extra_env_vars = {
  "TMI_WEBHOOK_ALLOW_HTTP_TARGETS" = "true"
  "TMI_ADMIN_PROVIDER"             = "tmi"
  "TMI_ADMIN_EMAIL"                = "charlie@tmi.local"
}

# ---------------------------------------------------------------------------
# Secrets
# ---------------------------------------------------------------------------
# Omitted — Terraform auto-generates random values:
#   db_password    (20 chars, special chars)
#   redis_password (24 chars, alphanumeric)
#   jwt_secret     (64 chars, alphanumeric)
# Generated values stored in Terraform state and OCI Vault.
```

- [ ] **Step 2: Verify terraform.tfvars is gitignored**

Check that `terraform.tfvars` is in `.gitignore` (it contains sensitive OCIDs):

```bash
cd /Users/efitz/Projects/tmi
grep -q 'terraform.tfvars' .gitignore && echo "OK: gitignored" || echo "WARN: not gitignored"
```

If not gitignored, add `terraform.tfvars` (but NOT `terraform.tfvars.example`) to `.gitignore`:

```bash
echo "terraform.tfvars" >> .gitignore
```

Note: Since this file contains infrastructure OCIDs, it should not be committed. The `terraform.tfvars.example` file (already in the repo) serves as the template.

- [ ] **Step 3: Validate the tfvars against variables.tf**

```bash
cd /Users/efitz/Projects/tmi/terraform/environments/oci-public
# Run terraform init first if .terraform/ directory does not exist
[ -d .terraform ] || terraform init
terraform validate
```

Expected: `Success! The configuration is valid.`

---

### Task 3: Create push-oci.sh for tmi-tf-wh

**Files:**
- Create: `/Users/efitz/Projects/tmi-tf-wh/scripts/push-oci.sh`
- Reference: `/Users/efitz/Projects/tmi-ux/scripts/push-oci.sh` (template to adapt)

This script is adapted from tmi-ux's `push-oci.sh` with these changes:
- Banner says "TMI-TF-WH" instead of "TMI-UX"
- Version extracted from `pyproject.toml` instead of `package.json`
- Uses `Dockerfile` (not `Dockerfile.oci`) since tmi-tf-wh's Dockerfile already uses Oracle Linux 9
- No `--build-arg APP_VERSION` (not used by tmi-tf-wh's Dockerfile)
- Passes `BUILD_DATE` and `GIT_COMMIT` build args (matching Dockerfile ARGs)
- Uses `docker buildx build` with `--push` for cross-platform builds (ARM64 on macOS); image size reporting unavailable with this pattern
- Adds `--profile` support via `OCI_CLI_PROFILE` env var (defaults to `tmi`); tmi-ux uses default OCI profile

- [ ] **Step 1: Create the scripts directory if needed**

```bash
ls /Users/efitz/Projects/tmi-tf-wh/scripts/ 2>/dev/null || mkdir -p /Users/efitz/Projects/tmi-tf-wh/scripts/
```

- [ ] **Step 2: Create push-oci.sh**

Create `/Users/efitz/Projects/tmi-tf-wh/scripts/push-oci.sh` with the following content:

```bash
#!/bin/bash
#
# push-oci.sh - Build and push TMI-TF-WH container to OCI Container Registry
#
# This script builds the TMI-TF-WH container image and pushes it to Oracle Cloud
# Infrastructure (OCI) Container Registry. Registry configuration (namespace,
# repository) is auto-discovered from OCI unless overridden via environment
# variables.
#
# Prerequisites:
#   - OCI CLI installed and configured (oci session authenticate or API key)
#   - Docker installed and running
#   - jq installed
#   - Access to the target OCI Container Repository
#
# Usage:
#   ./scripts/push-oci.sh [options]
#
# Options:
#   --region REGION       OCI region (default: us-ashburn-1)
#   --repo-ocid OCID      Container repository OCID (auto-discovered if not set)
#   --tag TAG             Image tag (default: latest)
#   --platform PLATFORM   Docker platform (default: linux/arm64)
#   --no-cache            Build without Docker cache
#   --help                Show this help message
#
# Environment Variables:
#   CONTAINER_REPO_OCID   Container repository OCID (alternative to --repo-ocid)
#   OCI_COMPARTMENT_ID    Compartment name or OCID to search for repos (searched first if set)
#   OCI_REGION            OCI region (alternative to --region)
#   OCI_TENANCY_NAMESPACE Override tenancy namespace (auto-detected if not set)
#   OCI_CLI_PROFILE       OCI CLI profile name (default: tmi)
#
# Example:
#   ./scripts/push-oci.sh
#   ./scripts/push-oci.sh --tag v0.1.0
#   OCI_COMPARTMENT_ID=tmi ./scripts/push-oci.sh --region us-phoenix-1 --no-cache
#

set -euo pipefail

# Script directory for relative paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# OCI CLI profile
OCI_PROFILE="${OCI_CLI_PROFILE:-tmi}"

# Logging functions - all output to stderr to avoid polluting command substitution
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

# Default values
REGION="${OCI_REGION:-us-ashburn-1}"
REPO_OCID="${CONTAINER_REPO_OCID:-}"
TAG="latest"
PLATFORM="linux/arm64"
NO_CACHE=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --region)
            REGION="$2"
            shift 2
            ;;
        --repo-ocid)
            REPO_OCID="$2"
            shift 2
            ;;
        --tag)
            TAG="$2"
            shift 2
            ;;
        --platform)
            PLATFORM="$2"
            shift 2
            ;;
        --no-cache)
            NO_CACHE=true
            shift
            ;;
        --help)
            sed -n '2,/^$/p' "$0" | sed 's/^# //' | sed 's/^#//'
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed or not in PATH"
        exit 1
    fi

    if ! docker info &> /dev/null; then
        log_error "Docker daemon is not running"
        exit 1
    fi

    if ! command -v oci &> /dev/null; then
        log_error "OCI CLI is not installed or not in PATH"
        log_info "Install it from: https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm"
        exit 1
    fi

    if ! command -v jq &> /dev/null; then
        log_error "jq is not installed or not in PATH"
        log_info "Install it: brew install jq"
        exit 1
    fi

    # Verify OCI CLI is configured
    if ! oci iam region list --profile "$OCI_PROFILE" --output json &> /dev/null; then
        log_error "OCI CLI is not configured for profile '${OCI_PROFILE}'. Run 'oci session authenticate' or configure API keys"
        exit 1
    fi

    log_success "All prerequisites met"
}

# Get tenancy namespace from OCI
get_tenancy_namespace() {
    if [[ -n "${OCI_TENANCY_NAMESPACE:-}" ]]; then
        echo "$OCI_TENANCY_NAMESPACE"
        return
    fi

    log_info "Fetching tenancy namespace from OCI..."
    local namespace
    namespace=$(oci os ns get --profile "$OCI_PROFILE" --query 'data' --raw-output 2>/dev/null)

    if [[ -z "$namespace" ]]; then
        log_error "Failed to get tenancy namespace from OCI"
        exit 1
    fi

    echo "$namespace"
}

# Search for container repositories in a compartment, return JSON array or empty
search_repos_in_compartment() {
    local comp_id="$1"
    local comp_name="${2:-}"
    if [[ -n "$comp_name" ]]; then
        log_info "Searching compartment: ${comp_name}..."
    fi
    oci artifacts container repository list \
        --profile "$OCI_PROFILE" \
        --compartment-id "$comp_id" \
        --query 'data.items[*].{name:"display-name",id:id}' \
        --output json 2>/dev/null || echo "[]"
}

# Prompt user to select a repo from a JSON array, sets REPO_OCID
select_repo_from_list() {
    local repos_json="$1"
    local repo_count
    repo_count=$(echo "$repos_json" | jq 'length')

    if [[ "$repo_count" -eq 1 ]]; then
        REPO_OCID=$(echo "$repos_json" | jq -r '.[0].id')
        REPO_NAME=$(echo "$repos_json" | jq -r '.[0].name')
        log_info "Auto-selected repository: ${REPO_NAME}"
    else
        log_info "Found ${repo_count} container repositories:"
        echo ""
        for i in $(seq 0 $((repo_count - 1))); do
            NAME=$(echo "$repos_json" | jq -r ".[$i].name")
            echo "  $((i + 1)). ${NAME}"
        done
        echo ""
        read -rp "Select repository [1-${repo_count}]: " SELECTION

        if [[ -z "$SELECTION" ]] || ! [[ "$SELECTION" =~ ^[0-9]+$ ]] || \
           [[ "$SELECTION" -lt 1 ]] || [[ "$SELECTION" -gt "$repo_count" ]]; then
            log_error "Invalid selection"
            exit 1
        fi

        local idx=$((SELECTION - 1))
        REPO_OCID=$(echo "$repos_json" | jq -r ".[$idx].id")
        REPO_NAME=$(echo "$repos_json" | jq -r ".[$idx].name")
        log_info "Selected repository: ${REPO_NAME}"
    fi
}

# Auto-discover container repository OCID
discover_repo() {
    log_info "CONTAINER_REPO_OCID not set, discovering from OCI..."

    # Try OCI_COMPARTMENT_ID first for targeted search (accepts OCID or name)
    local compartment_id="${OCI_COMPARTMENT_ID:-}"
    local repos_json="[]"
    if [[ -n "$compartment_id" ]]; then
        # Resolve compartment name to OCID if not already an OCID
        if [[ "$compartment_id" != ocid1.compartment.* ]]; then
            log_info "Resolving compartment name '${compartment_id}' to OCID..."
            local resolved_id
            resolved_id=$(oci iam compartment list \
                --profile "$OCI_PROFILE" \
                --compartment-id-in-subtree true \
                --access-level ACCESSIBLE \
                --lifecycle-state ACTIVE \
                --query "data[?name=='${compartment_id}'].id | [0]" \
                --raw-output 2>/dev/null || true)
            if [[ -n "$resolved_id" && "$resolved_id" != "null" ]]; then
                log_info "Resolved compartment '${compartment_id}' to ${resolved_id}"
                compartment_id="$resolved_id"
            else
                log_warn "Could not resolve compartment name '${compartment_id}', falling back to tenancy search"
                compartment_id=""
            fi
        fi
        if [[ -n "$compartment_id" ]]; then
            log_info "Searching compartment ${compartment_id}..."
            repos_json=$(search_repos_in_compartment "$compartment_id")
        fi
    fi

    # If no compartment found or no repos in it, search tenancy + child compartments
    if [[ -z "$compartment_id" ]] || [[ "$repos_json" == "[]" || "$repos_json" == "null" || -z "$repos_json" ]]; then
        log_info "No repos found in target compartment, searching tenancy..."

        # Get tenancy OCID (root compartment)
        TENANCY_OCID=$(oci iam compartment list --profile "$OCI_PROFILE" --query 'data[0]."compartment-id"' --raw-output 2>/dev/null || true)
        if [[ -z "$TENANCY_OCID" ]]; then
            log_error "Could not determine tenancy. Set CONTAINER_REPO_OCID, OCI_COMPARTMENT_ID, or use --repo-ocid"
            exit 1
        fi

        # Search root compartment
        repos_json=$(search_repos_in_compartment "$TENANCY_OCID" "root tenancy")

        # If not found in root, search child compartments
        if [[ "$repos_json" == "[]" || "$repos_json" == "null" || -z "$repos_json" ]]; then
            log_info "No repos in root tenancy, searching child compartments..."

            local compartments_json
            compartments_json=$(oci iam compartment list \
                --profile "$OCI_PROFILE" \
                --compartment-id "$TENANCY_OCID" \
                --compartment-id-in-subtree true \
                --access-level ACCESSIBLE \
                --lifecycle-state ACTIVE \
                --query 'data[*].{name:name,id:id}' \
                --output json 2>/dev/null || echo "[]")

            local comp_count
            comp_count=$(echo "$compartments_json" | jq 'length')
            for i in $(seq 0 $((comp_count - 1))); do
                local comp_id comp_name
                comp_id=$(echo "$compartments_json" | jq -r ".[$i].id")
                comp_name=$(echo "$compartments_json" | jq -r ".[$i].name")
                repos_json=$(search_repos_in_compartment "$comp_id" "$comp_name")
                if [[ "$repos_json" != "[]" && "$repos_json" != "null" && -n "$repos_json" ]]; then
                    break
                fi
            done
        fi
    fi

    # Validate we found repos
    if [[ -z "$repos_json" || "$repos_json" == "[]" || "$repos_json" == "null" ]]; then
        log_error "No container repositories found in any compartment"
        log_info "Create one in OCI Console > Developer Services > Container Registry"
        exit 1
    fi

    # Select repo
    select_repo_from_list "$repos_json"

    log_info "Repository OCID: ${REPO_OCID}"
    export CONTAINER_REPO_OCID="$REPO_OCID"
}

# Get repository name from OCID
get_repo_name() {
    log_info "Fetching repository details from OCI..."
    local repo_name
    repo_name=$(oci artifacts container repository get \
        --profile "$OCI_PROFILE" \
        --repository-id "$REPO_OCID" \
        --query 'data."display-name"' \
        --raw-output 2>/dev/null)

    if [[ -z "$repo_name" ]]; then
        log_error "Failed to get repository name from OCID: $REPO_OCID"
        exit 1
    fi

    echo "$repo_name"
}

# Authenticate with OCI Container Registry
authenticate_ocir() {
    local registry="$1"
    local namespace="$2"

    log_info "Authenticating with OCI Container Registry..."

    # Try to use docker credential helper if available
    if docker-credential-oci-container-registry list &> /dev/null 2>&1; then
        log_info "Using OCI credential helper for Docker authentication"
        return 0
    fi

    # Check if already logged in
    if docker login "${registry}" --get-login &> /dev/null 2>&1; then
        log_info "Already authenticated with ${registry}"
        return 0
    fi

    # For session-based auth, prompt for interactive login
    log_warn "Docker login to OCI Container Registry required"
    log_info "To authenticate, you need an OCI Auth Token:"
    log_info "  1. Go to OCI Console > Identity > Users > Your User > Auth Tokens"
    log_info "  2. Generate a new token (save it, shown only once)"
    log_info "  3. Run: docker login ${registry}"
    log_info "     Username: ${namespace}/your-email@example.com"
    log_info "     Password: your-auth-token"
    log_info ""
    log_info "Attempting interactive login..."

    if ! docker login "${registry}"; then
        log_error "Failed to authenticate with OCI Container Registry"
        exit 1
    fi

    log_success "Authenticated with OCI Container Registry"
}

# Get version from pyproject.toml
get_app_version() {
    local version
    version=$(grep '^version' "${PROJECT_ROOT}/pyproject.toml" | head -1 | sed 's/.*"\(.*\)".*/\1/')
    if [[ -z "$version" ]]; then
        log_warn "Could not extract version from pyproject.toml, using 'dev'"
        version="dev"
    fi
    echo "$version"
}

# Main execution
main() {
    log_info "TMI-TF-WH OCI Container Registry Deployment"
    log_info "============================================"

    # Check prerequisites
    check_prerequisites

    # Auto-discover repository if not provided
    if [[ -z "$REPO_OCID" ]]; then
        discover_repo
    fi

    # Get OCI configuration
    local namespace
    namespace=$(get_tenancy_namespace)
    log_info "Tenancy namespace: ${namespace}"

    local repo_name
    repo_name=$(get_repo_name)
    log_info "Repository name: ${repo_name}"

    # Construct image name
    local registry="${REGION}.ocir.io"
    local full_image_name="${registry}/${namespace}/${repo_name}:${TAG}"
    log_info "Image: ${full_image_name}"

    # Get version from pyproject.toml
    local app_version
    app_version=$(get_app_version)
    log_info "App version: ${app_version}"

    # Authenticate with OCIR
    authenticate_ocir "$registry" "$namespace"

    # Build the image
    log_info "Building Docker image for OCI..."
    local build_date
    build_date=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local git_commit
    git_commit=$(git -C "${PROJECT_ROOT}" rev-parse --short HEAD 2>/dev/null || echo "unknown")

    local build_args=(
        --platform "${PLATFORM}"
        --file "${PROJECT_ROOT}/Dockerfile"
        --tag "${full_image_name}"
        --build-arg "BUILD_DATE=${build_date}"
        --build-arg "GIT_COMMIT=${git_commit}"
        --push
    )

    if [[ "$TAG" == "latest" ]]; then
        build_args+=(--tag "${registry}/${namespace}/${repo_name}:v${app_version}")
    fi

    if [[ "$NO_CACHE" == true ]]; then
        build_args+=(--no-cache)
    fi

    if ! docker buildx build "${build_args[@]}" "${PROJECT_ROOT}"; then
        log_error "Docker build failed"
        exit 1
    fi

    log_success "Build and push complete!"
    log_info "Image pushed to: ${full_image_name}"
    if [[ "$TAG" == "latest" ]]; then
        log_info "Version tag: ${registry}/${namespace}/${repo_name}:v${app_version}"
    fi
}

# Run main
main "$@"
```

- [ ] **Step 3: Make the script executable**

```bash
chmod +x /Users/efitz/Projects/tmi-tf-wh/scripts/push-oci.sh
```

- [ ] **Step 4: Verify script syntax**

```bash
bash -n /Users/efitz/Projects/tmi-tf-wh/scripts/push-oci.sh && echo "Syntax OK"
```

Expected: `Syntax OK`

- [ ] **Step 5: Test --help output**

```bash
/Users/efitz/Projects/tmi-tf-wh/scripts/push-oci.sh --help
```

Expected: Help text showing usage, options, and environment variables.

- [ ] **Step 6: Commit to tmi-tf-wh repo**

```bash
cd /Users/efitz/Projects/tmi-tf-wh
git add scripts/push-oci.sh
git commit -m "$(cat <<'EOF'
feat: add OCI container registry push script

Standalone bash script to build and push tmi-tf-wh container to OCIR.
Follows the same pattern as tmi-ux push-oci.sh: auto-discovers OCIR
repos, authenticates with Docker credential helper or interactive login,
builds ARM64 image with docker buildx, and pushes with latest + version
tags.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Lint and Validate

- [ ] **Step 1: Lint TMI repo**

```bash
cd /Users/efitz/Projects/tmi
make lint
```

Expected: No new lint errors from our Terraform changes.

- [ ] **Step 2: Build TMI server**

```bash
cd /Users/efitz/Projects/tmi
make build-server
```

Expected: Successful build (no Go code changed, but validates nothing is broken).

- [ ] **Step 3: Run unit tests**

```bash
cd /Users/efitz/Projects/tmi
make test-unit
```

Expected: All tests pass. No Go code was changed so this is a sanity check.

- [ ] **Step 4: Lint tmi-tf-wh repo**

```bash
cd /Users/efitz/Projects/tmi-tf-wh
uv run ruff check .
```

Expected: No lint errors in the push-oci.sh (ruff is Python-only; the shell script won't be checked by it, but this validates no accidental Python changes).

- [ ] **Step 5: Push TMI repo changes**

```bash
cd /Users/efitz/Projects/tmi
git pull --rebase
git push
git status
```

Expected: `Your branch is up to date with 'origin/release/1.3.0'`

- [ ] **Step 6: Push tmi-tf-wh repo changes**

```bash
cd /Users/efitz/Projects/tmi-tf-wh
git pull --rebase
git push
git status
```

Expected: Up to date with remote. (Commits go to whatever branch is currently checked out in tmi-tf-wh.)
