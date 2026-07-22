#!/bin/bash
# scripts/deploy-aws.sh
#
# Deploy TMI to AWS: EKS + RDS + Secrets Manager, with workloads applied via
# kubectl/kustomize (not Terraform). This is a *hybrid* flow:
#
#   preflight -> terraform apply (infra only, including the 5 ECR repos)
#   -> ECR login + build/push 5 images -> EKS kubeconfig
#   -> apply platform base (NATS/KEDA/CRD)
#   -> apply the AWS kustomize overlay (with placeholder substitution)
#   -> upsert the server CNAME -> optional dbtool config import
#   -> verify
#
# Terraform (terraform/environments/aws-public) is the SOLE owner of the 5
# ECR repositories (server, redis, extractor, chunkembed, controller) as
# well as the rest of the infrastructure: VPC, EKS cluster/node group, RDS
# Postgres, IAM/IRSA, ACM certificate, and bootstrap Kubernetes objects
# (namespace, the tmi-server-config ConfigMap, secrets, the tmi-api
# ServiceAccount). Terraform applies first, so the ECR repos already exist
# by the time images are built/pushed — this script never creates ECR repos
# itself. This script owns every workload (server, redis, controller,
# extractor, chunkembed, ingress) via deployments/k8s/dev/aws, mirroring the
# ownership split documented in deployments/k8s/dev/aws/README.md.
# --dry-run stops after `terraform plan`; it never builds or pushes images
# or touches the cluster.
#
# Usage: ./scripts/deploy-aws.sh --domain <fqdn> --zone-id <zone> [options]
#
# Options:
#   --region REGION               AWS region (default: us-east-1)
#   --name-prefix PREFIX          Resource name prefix (default: tmi)
#   --profile PROFILE             AWS CLI/SDK profile, exported as AWS_PROFILE (default: tmi)
#   --domain DOMAIN                Domain name for the ACM certificate and server CNAME (required)
#   --zone-id ZONE_ID              Route 53 hosted zone ID for --domain (required)
#   --config-export FILE           Import this dbtool config-export YAML into the deployed
#                                   database after the overlay is up (optional)
#   --skip-build                   Skip container image build/push (use existing ECR images)
#   --destroy                      Destroy the deployment instead of creating it (--domain/--zone-id still required)
#   --dry-run                      Run terraform plan only (no apply, no build/push, no cluster changes)
#   --auto-approve                 Skip terraform apply confirmation
#   --help                         Show this help message
#
# Environment variables:
#   TMI_EMBEDDING_API_KEY          API key for the chunk-embed worker's embedding
#                                   provider (Secret/tmi-embedding, key api-key). If
#                                   unset, the secret is NOT created and a warning is
#                                   printed — chunk-embed will fail with
#                                   CreateContainerConfigError when KEDA scales it up
#                                   from zero. Not stored anywhere by this script;
#                                   never echoed or logged.
#
# Examples:
#   ./scripts/deploy-aws.sh --domain tmi.example.com --zone-id Z1234567890ABC
#   ./scripts/deploy-aws.sh --domain tmi.example.com --zone-id Z1234567890ABC --skip-build --dry-run
#   ./scripts/deploy-aws.sh --domain tmi.example.com --zone-id Z1234567890ABC --config-export /tmp/tmi-config.yaml
#   ./scripts/deploy-aws.sh --destroy --domain tmi.example.com --zone-id Z1234567890ABC
#
# Removed flags (no longer apply — see terraform/environments/aws-public/variables.tf):
#   --san, --alert-email, --db-instance-class, --db-multi-az, --multi-az-nat
#   These fed Terraform variables that do not exist in the current aws-public
#   environment (db instance class, Multi-AZ, NAT HA, and SAN are not
#   deployer-configurable there today). Passing them now fails fast with an
#   explanation instead of silently doing nothing.

set -euo pipefail

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Logging functions
log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()    { echo -e "\n${BOLD}=== $1 ===${NC}\n"; }

# Script directory and project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TF_DIR="${PROJECT_ROOT}/terraform/environments/aws-public"
PLATFORM_DIR="${PROJECT_ROOT}/deployments/k8s/platform"
OVERLAY_DIR="${PROJECT_ROOT}/deployments/k8s/dev/aws"
NAMESPACE="tmi-platform"

# Default values
REGION="us-east-1"
NAME_PREFIX="tmi"
PROFILE="tmi"
DOMAIN=""
ZONE_ID=""
CONFIG_EXPORT_FILE=""
SKIP_BUILD=false
DESTROY=false
DRY_RUN=false
AUTO_APPROVE=false

# Populated as the script progresses; used by cleanup() on exit.
PF_PID=""
RDS_PROXY_STARTED=false
TMPDIR_TO_CLEAN=""

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --region)
                REGION="$2"; shift 2 ;;
            --name-prefix)
                NAME_PREFIX="$2"; shift 2 ;;
            --profile)
                PROFILE="$2"; shift 2 ;;
            --domain)
                DOMAIN="$2"; shift 2 ;;
            --zone-id)
                ZONE_ID="$2"; shift 2 ;;
            --config-export)
                CONFIG_EXPORT_FILE="$2"; shift 2 ;;
            --skip-build)
                SKIP_BUILD=true; shift ;;
            --destroy)
                DESTROY=true; shift ;;
            --dry-run)
                DRY_RUN=true; shift ;;
            --auto-approve)
                AUTO_APPROVE=true; shift ;;
            --san|--alert-email|--db-instance-class|--db-multi-az|--multi-az-nat)
                log_error "Flag $1 is no longer supported."
                echo "  terraform/environments/aws-public/variables.tf has no backing variable"
                echo "  for it (subject_alternative_names / alert_email / db instance class /"
                echo "  Multi-AZ / NAT HA are not deployer-configurable in this environment)."
                echo "  See the removed-flags note in 'scripts/deploy-aws.sh --help'."
                exit 1 ;;
            --help|-h)
                show_help; exit 0 ;;
            *)
                log_error "Unknown option: $1"
                echo "Use --help for usage information."
                exit 1 ;;
        esac
    done
}

show_help() {
    sed -n '2,64p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

# ============================================================================
# Cleanup (runs on any exit)
# ============================================================================

cleanup() {
    local exit_code=$?
    if [[ -n "${PF_PID}" ]]; then
        kill "${PF_PID}" 2>/dev/null || true
    fi
    if [[ "${RDS_PROXY_STARTED}" == "true" ]]; then
        kubectl delete pod rds-proxy -n "${NAMESPACE}" --ignore-not-found >/dev/null 2>&1 || true
    fi
    if [[ -n "${TMPDIR_TO_CLEAN}" ]] && [[ -d "${TMPDIR_TO_CLEAN}" ]]; then
        rm -rf "${TMPDIR_TO_CLEAN}"
    fi
    rm -f "${TF_DIR}/tfplan"
    return "${exit_code}"
}
trap cleanup EXIT

# ============================================================================
# PHASE 1: Preflight Checks
# ============================================================================

preflight_checks() {
    log_step "Phase 1: Preflight Checks"

    local failed=0

    export AWS_PROFILE="${PROFILE}"

    # 1. --name-prefix is currently required to be "tmi": the build helper
    # (scripts/container_build_helpers.py's aws image_name_prefix), the
    # deployments/k8s/dev/aws kustomize overlay (hardcoded ECR repo/image
    # names), and the Secret/ConfigMap names this script and the overlay
    # reference (tmi-secrets, tmi-server-config, tmi-embedding) are all
    # hardcoded to "tmi", not templated from --name-prefix. Passing a
    # different prefix would desync Terraform's ECR repo names from what the
    # build/push and kustomize steps push to and expect. Flag kept for future
    # work (templating those three coupling points).
    if [[ "${NAME_PREFIX}" != "tmi" ]]; then
        log_error "--name-prefix must be \"tmi\" (got: ${NAME_PREFIX})"
        echo "  The build helper, the deployments/k8s/dev/aws kustomize overlay, and the"
        echo "  Secret/ConfigMap names it references are hardcoded to the \"tmi\" prefix."
        echo "  The flag is kept for future work; do not pass a different value yet."
        failed=1
    fi

    # 2. AWS CLI
    if command -v aws &>/dev/null; then
        local aws_version
        aws_version=$(aws --version 2>&1 | awk '{print $1}')
        log_success "AWS CLI installed (${aws_version})"
    else
        log_error "AWS CLI not installed"
        echo "  Install: brew install awscli"
        failed=1
    fi

    # 3. AWS credentials (respects --profile via AWS_PROFILE)
    if aws sts get-caller-identity &>/dev/null; then
        local account_id identity
        account_id=$(aws sts get-caller-identity --query Account --output text)
        identity=$(aws sts get-caller-identity --query Arn --output text)
        log_success "AWS credentials valid (profile: ${PROFILE}, account: ${account_id}, identity: ${identity})"
    else
        log_error "AWS credentials not configured or expired for profile '${PROFILE}'"
        echo "  Configure: aws configure --profile ${PROFILE}"
        echo "  For SSO:   aws sso login --profile ${PROFILE}"
        failed=1
    fi

    # 4. Terraform
    if command -v terraform &>/dev/null; then
        local tf_version
        tf_version=$(terraform version -json 2>/dev/null | jq -r '.terraform_version' 2>/dev/null || terraform version | head -1)
        log_success "Terraform installed (${tf_version})"

        local major minor
        major=$(echo "${tf_version}" | cut -d. -f1)
        minor=$(echo "${tf_version}" | cut -d. -f2)
        if [[ "${major}" -lt 1 ]] || { [[ "${major}" -eq 1 ]] && [[ "${minor}" -lt 5 ]]; }; then
            log_error "Terraform >= 1.5.0 required (found ${tf_version})"
            failed=1
        fi
    else
        log_error "Terraform not installed"
        echo "  Install: brew install terraform"
        failed=1
    fi

    # 5. Docker (unless skipping build)
    if [[ "${SKIP_BUILD}" == "false" ]] && [[ "${DESTROY}" == "false" ]]; then
        if command -v docker &>/dev/null; then
            if docker info &>/dev/null; then
                log_success "Docker installed and running"
            else
                log_error "Docker is installed but not running"
                echo "  Start Docker Desktop or run: open -a Docker"
                failed=1
            fi
        else
            log_error "Docker not installed (required for container builds)"
            echo "  Or skip:  --skip-build (if images are already in ECR)"
            failed=1
        fi
    else
        log_info "Skipping Docker check (--skip-build or --destroy)"
    fi

    # 6. uv (drives build-app-containers.py)
    if [[ "${SKIP_BUILD}" == "false" ]] && [[ "${DESTROY}" == "false" ]]; then
        if command -v uv &>/dev/null; then
            log_success "uv installed"
        else
            log_error "uv not installed (required to run scripts/build-app-containers.py)"
            echo "  Install: https://docs.astral.sh/uv/getting-started/installation/"
            failed=1
        fi
    fi

    # 7. kubectl
    if command -v kubectl &>/dev/null; then
        log_success "kubectl installed"
    else
        log_error "kubectl not installed (required to apply the platform base and overlay)"
        echo "  Install: brew install kubectl"
        failed=1
    fi

    # 8. jq
    if command -v jq &>/dev/null; then
        log_success "jq installed"
    else
        log_error "jq not installed (required by this script)"
        echo "  Install: brew install jq"
        failed=1
    fi

    # 9. Terraform directory exists
    if [[ -d "${TF_DIR}" ]]; then
        log_success "Terraform config found at terraform/environments/aws-public/"
    else
        log_error "Terraform config not found at ${TF_DIR}"
        echo "  Ensure you are running from the TMI project root"
        failed=1
    fi

    # 10. Backend config exists (terraform init -backend-config=...)
    if [[ -d "${TF_DIR}" ]]; then
        local backend_config="${BACKEND_CONFIG:-${TF_DIR}/backend.hcl}"
        if [[ -f "${backend_config}" ]]; then
            log_success "Terraform backend config found: ${backend_config}"
        else
            log_error "Terraform backend config not found: ${backend_config}"
            echo "  Create it from the example in terraform/environments/aws-public/main.tf's"
            echo "  backend comment block, e.g.:"
            echo "    bucket         = \"tmi-deployer-tfstate\""
            echo "    region         = \"us-east-1\""
            echo "    dynamodb_table = \"tmi-tf-locks\""
            echo "  Or set BACKEND_CONFIG=/path/to/backend.hcl"
            failed=1
        fi
    fi

    # 11. Overlay directory exists
    if [[ -d "${OVERLAY_DIR}" ]]; then
        log_success "AWS kustomize overlay found at deployments/k8s/dev/aws/"
    else
        log_error "AWS kustomize overlay not found at ${OVERLAY_DIR}"
        failed=1
    fi

    # 12. --domain / --zone-id required for deploy, dry-run, AND destroy:
    # terraform/environments/aws-public/variables.tf declares domain_name and
    # hosted_zone_id with no default, so `terraform destroy` needs a tfvars
    # file supplying them just as much as `terraform apply` does.
    if [[ -z "${DOMAIN}" ]] || [[ -z "${ZONE_ID}" ]]; then
        log_error "--domain and --zone-id are both required (including for --destroy)"
        echo "  domain_name/hosted_zone_id are required Terraform variables with no"
        echo "  default; Terraform needs them to plan/apply OR destroy this environment."
        failed=1
    fi

    # 13. Check AWS region is valid
    if [[ "${failed}" -eq 0 ]]; then
        if aws ec2 describe-regions --region-names "${REGION}" &>/dev/null; then
            log_success "AWS region '${REGION}' is valid"
        else
            log_error "Invalid AWS region: ${REGION}"
            failed=1
        fi
    fi

    if [[ "${failed}" -ne 0 ]]; then
        echo ""
        log_error "Preflight checks failed. Please fix the issues above and retry."
        exit 1
    fi

    log_success "All preflight checks passed"
}

# ============================================================================
# PHASE 2: Terraform (infrastructure only, including the 5 ECR repos)
# ============================================================================

terraform_init() {
    log_info "Initializing Terraform..."
    terraform -chdir="${TF_DIR}" init -backend-config="${BACKEND_CONFIG:-${TF_DIR}/backend.hcl}"
    log_success "Terraform initialized"
}

terraform_deploy() {
    log_step "Phase 2: Terraform Infrastructure ${DESTROY:+(Destroy)}"

    terraform_init

    # domain_name/hosted_zone_id have no default in variables.tf, so both the
    # deploy and the destroy path need terraform.tfvars generated first —
    # one code path, not a throwaway -var=... special case for destroy.
    generate_tfvars

    if [[ "${DESTROY}" == "true" ]]; then
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_info "Dry run: showing destroy plan..."
            terraform -chdir="${TF_DIR}" plan -destroy
        else
            pre_destroy_cleanup
            log_warning "This will DESTROY all TMI AWS infrastructure (including the ECR repos)!"
            if [[ "${AUTO_APPROVE}" == "true" ]]; then
                terraform -chdir="${TF_DIR}" destroy -auto-approve
            else
                terraform -chdir="${TF_DIR}" destroy
            fi
            log_success "Infrastructure destroyed"
        fi
        return 0
    fi

    log_info "Running terraform plan..."
    terraform -chdir="${TF_DIR}" plan -out=tfplan
    log_success "Plan complete"

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "Dry run complete. Review the plan above."
        log_info "To apply (and build/push images, and deploy), run without --dry-run"
        return 0
    fi

    log_info "Applying Terraform plan (creates/updates infra, including the 5 ECR repos)..."
    if [[ "${AUTO_APPROVE}" == "true" ]]; then
        terraform -chdir="${TF_DIR}" apply tfplan
    else
        echo ""
        echo -e "${YELLOW}Review the plan above. Terraform will prompt for confirmation.${NC}"
        echo ""
        terraform -chdir="${TF_DIR}" apply tfplan
    fi

    log_success "Terraform apply complete"
}

# Cluster-side objects the ALB controller and this script create at runtime
# (the ALB behind ingress/tmi-server, and the Route 53 CNAME upserted in
# Phase 6) are NOT terraform-managed — if left behind they orphan a load
# balancer with ENIs still attached to the VPC, which can make `terraform
# destroy` hang or fail deleting the VPC/subnets/security groups. Best-effort
# only: every step tolerates absence or an unreachable cluster (`|| true`
# equivalents), since destroy must still proceed even if the cluster was
# already torn down out-of-band.
pre_destroy_cleanup() {
    log_step "Phase 2.5: Pre-Destroy Cleanup (best-effort)"

    local cluster_name
    cluster_name=$(terraform -chdir="${TF_DIR}" output -raw cluster_name 2>/dev/null || true)
    if [[ -z "${cluster_name}" ]]; then
        log_info "No cluster_name in Terraform state (nothing deployed yet?) — skipping cluster cleanup"
        return 0
    fi

    aws eks update-kubeconfig --name "${cluster_name}" --region "${REGION}" &>/dev/null || true
    if ! kubectl cluster-info &>/dev/null; then
        log_info "Cluster ${cluster_name} not reachable — skipping ingress/ALB/DNS cleanup"
        return 0
    fi

    local alb_host
    alb_host=$(kubectl get ingress tmi-server -n "${NAMESPACE}" \
        -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)

    log_info "Deleting ingress/tmi-server (releases the ALB)..."
    kubectl delete ingress tmi-server -n "${NAMESPACE}" --ignore-not-found &>/dev/null || true

    if [[ -n "${alb_host}" ]]; then
        log_info "Waiting up to 5 minutes for ALB (${alb_host}) to be deprovisioned..."
        local waited=0 lb_count
        while [[ "${waited}" -lt 300 ]]; do
            lb_count=$(aws elbv2 describe-load-balancers --region "${REGION}" \
                --query "length(LoadBalancers[?DNSName=='${alb_host}'])" --output text 2>/dev/null || echo "0")
            if [[ "${lb_count}" == "0" ]]; then
                log_success "ALB deprovisioned"
                break
            fi
            sleep 10
            waited=$((waited + 10))
        done
    else
        log_info "No ALB hostname on ingress/tmi-server — nothing to wait for"
    fi

    log_info "Removing Route 53 CNAME for ${DOMAIN}..."
    local existing_value
    existing_value=$(aws route53 list-resource-record-sets --hosted-zone-id "${ZONE_ID}" \
        --query "ResourceRecordSets[?Name=='${DOMAIN}.' && Type=='CNAME'].ResourceRecords[0].Value | [0]" \
        --output text 2>/dev/null || true)
    if [[ -n "${existing_value}" ]] && [[ "${existing_value}" != "None" ]]; then
        if aws route53 change-resource-record-sets --hosted-zone-id "${ZONE_ID}" \
            --change-batch "{\"Changes\":[{\"Action\":\"DELETE\",\"ResourceRecordSet\":{
              \"Name\":\"${DOMAIN}\",\"Type\":\"CNAME\",\"TTL\":300,
              \"ResourceRecords\":[{\"Value\":\"${existing_value}\"}]}}]}" &>/dev/null; then
            log_success "Route 53 CNAME removed"
        else
            log_warning "Failed to remove Route 53 CNAME for ${DOMAIN} (continuing destroy)"
        fi
    else
        log_info "No CNAME found for ${DOMAIN} — nothing to remove"
    fi
}

generate_tfvars() {
    log_info "Generating terraform.tfvars (gitignored, will not be committed)..."

    cat > "${TF_DIR}/terraform.tfvars" <<TFVARS
# Auto-generated by deploy-aws.sh — this file is gitignored
# Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

aws_region  = "${REGION}"
name_prefix = "${NAME_PREFIX}"

# Certificate (ACM) + DNS
domain_name    = "${DOMAIN}"
hosted_zone_id = "${ZONE_ID}"
TFVARS

    log_success "Generated terraform.tfvars"
}

capture_terraform_outputs() {
    log_info "Capturing Terraform outputs..."
    ECR_URLS_JSON=$(terraform -chdir="${TF_DIR}" output -json ecr_repository_urls)
    CERT_ARN=$(terraform -chdir="${TF_DIR}" output -raw certificate_arn)
    CLUSTER_NAME=$(terraform -chdir="${TF_DIR}" output -raw cluster_name)
    RDS_ENDPOINT=$(terraform -chdir="${TF_DIR}" output -raw rds_endpoint)
    ECR_REGISTRY=$(echo "${ECR_URLS_JSON}" | jq -r '.server' | sed 's|/[^/]*$||')
    log_success "cluster_name=${CLUSTER_NAME}"
}

# ============================================================================
# PHASE 3: Container Images (ECR login + build/push, after infra exists)
# ============================================================================

build_and_push_images() {
    log_step "Phase 3: Container Images"

    if [[ "${SKIP_BUILD}" == "true" ]]; then
        log_info "Skipping build/push (--skip-build); assuming images already exist in ECR"
        return 0
    fi

    # Terraform (Phase 2) already created the 5 ECR repos this script pushes
    # to — Terraform is their sole owner, so nothing here creates or manages
    # ECR repositories. build-app-containers.py authenticates to ECR itself
    # (aws ecr get-login-password | docker login, see
    # scripts/container_build_helpers.py's _aws_ecr_login()) before pushing,
    # so no separate `docker login` call is needed here.
    log_info "Building and pushing all 5 images (server, redis, extractor, chunkembed, controller)..."
    # No `--build-tags dev`: this is an internet-facing deployment, so the
    # server is built WITHOUT the dev-only "tmi" stub OAuth provider (which
    # performs no credential check and mints users from login_hint). Under the
    # default (production) build tag the tmi provider is client-credentials
    # only — there is no anonymous authorization-code/login_hint path — and an
    # explicit `idp` is required. Interactive auth comes from the real OAuth
    # providers (e.g. Google) carried in the replicated database config. Do NOT
    # add `--build-tags dev` here: it would recompile the no-auth stub into a
    # public binary.
    (cd "${PROJECT_ROOT}" && uv run "${SCRIPT_DIR}/build-app-containers.py" \
        --target aws --component all --push --scan)
    log_success "All images built, pushed, and scanned"
}

configure_kubeconfig() {
    log_info "Configuring kubectl for EKS cluster ${CLUSTER_NAME}..."
    aws eks update-kubeconfig --name "${CLUSTER_NAME}" --region "${REGION}"
    log_success "kubectl configured"
}

# ============================================================================
# PHASE 4: Kubernetes Platform Base
# ============================================================================

apply_platform_base() {
    log_step "Phase 4: Platform Base (NATS, KEDA, TMIComponent CRD)"

    kubectl apply -f "${PLATFORM_DIR}/nats.yml"
    kubectl apply --server-side -f "${PLATFORM_DIR}/keda.yml"
    kubectl apply -f "${PROJECT_ROOT}/config/crd/bases/tmi.dev_tmicomponents.yaml"

    log_success "Platform base applied"
}

# ============================================================================
# PHASE 4.5: Chunk-Embed API Key Secret
# ============================================================================

# Mirrors create_embedding_secret() in scripts/lib/deploy.py:478 (create-or-
# update via `kubectl create --dry-run=client -o yaml | kubectl apply -f -`,
# key never echoed) but does NOT fall back to a placeholder value the way the
# local-dev helper does: on AWS a placeholder key would deploy successfully
# and fail invisibly later (bad embedding-API auth) rather than obviously, so
# instead we skip creating the Secret and warn loudly. The chunk-embed
# TMIComponent (deployments/k8s/platform/components/tmi-chunk-embed.yml)
# references Secret/tmi-embedding's `api-key` key via secretKeyRef; if the
# Secret doesn't exist, the pod KEDA scales up from zero fails immediately
# with CreateContainerConfigError.
create_embedding_secret() {
    log_step "Phase 4.5: Chunk-Embed API Key Secret"

    if [[ -z "${TMI_EMBEDDING_API_KEY:-}" ]]; then
        log_warning "TMI_EMBEDDING_API_KEY is not set — Secret/tmi-embedding will NOT be created."
        log_warning "chunk-embed (deployments/k8s/platform/components/tmi-chunk-embed.yml) reads"
        log_warning "its embedding-provider API key from this Secret's 'api-key' key via secretKeyRef."
        log_warning "When KEDA scales chunk-embed up from zero without it, the pod will fail with"
        log_warning "CreateContainerConfigError. Set TMI_EMBEDDING_API_KEY and re-run to fix."
        return 0
    fi

    log_info "Creating/updating Secret/tmi-embedding (key never logged)..."
    local rendered
    rendered=$(kubectl create secret generic tmi-embedding -n "${NAMESPACE}" \
        --from-literal="api-key=${TMI_EMBEDDING_API_KEY}" \
        --dry-run=client -o yaml)
    echo "${rendered}" | kubectl apply -f - >/dev/null
    log_success "Secret/tmi-embedding created/updated"
}

# ============================================================================
# PHASE 5: Application Overlay
# ============================================================================

apply_overlay() {
    log_step "Phase 5: AWS Kustomize Overlay"

    # Rewrite account-specific placeholders into the rendered kustomize
    # stream (never onto tracked files on disk, and never as generated files
    # written into the overlay directory) and apply.
    #
    # Exactly two placeholder tokens exist in the overlay (see
    # deployments/k8s/dev/aws/README.md): CERT_ARN_PLACEHOLDER (ingress.yml's
    # ACM certificate-arn annotation) and ECR_REGISTRY_PLACEHOLDER (the
    # top-level `images:` transformer in kustomization.yaml, which pins
    # tmi-server, tmi-component-controller, and tmi-redis, plus the two
    # TMIComponent JSON6902 patches for tmi-extractor/tmi-chunk-embed). All
    # five workload images are covered by that transformer/patches as of
    # commit 126782e0 — this script does not need, and must not add, any
    # further image-rewrite substitutions.
    #
    # Tag tradeoff: every image resolves to ECR_REGISTRY_PLACEHOLDER/tmi-
    # <component>:latest, a mutable tag shared by every deploy. No manifest in
    # this overlay sets imagePullPolicy, so Kubernetes' default applies —
    # which is Always for a ":latest" tag — so a pod that gets (re)scheduled
    # always pulls the freshly-pushed image. That's acceptable for this "kick
    # the tires" environment, but it means two concurrent deploys or a slow
    # rollout can observe a moving target, and there's no way to pin/rollback
    # to a specific past build by tag alone. Per-deploy immutable tags (e.g.
    # the git commit SHA already computed by build-app-containers.py) are a
    # follow-up, not implemented here.
    #
    # imagePullPolicy: Always only helps a pod that gets (re)scheduled — it
    # does NOT make `kubectl apply` roll a Deployment whose spec is otherwise
    # unchanged (same image ref, same tag). So a re-deploy that only refreshed
    # the ":latest" image contents needs an explicit rollout restart below;
    # see the "detect first install" logic and the restart calls following
    # this apply.
    local server_existed=false
    if kubectl get deployment tmi-server -n "${NAMESPACE}" &>/dev/null; then
        server_existed=true
    fi

    log_info "Rendering and applying overlay (ECR registry: ${ECR_REGISTRY})..."
    sed -e "s|CERT_ARN_PLACEHOLDER|${CERT_ARN}|" \
        -e "s|ECR_REGISTRY_PLACEHOLDER|${ECR_REGISTRY}|" \
        <(kubectl kustomize --load-restrictor LoadRestrictionsNone "${OVERLAY_DIR}") \
      | kubectl apply -f -

    log_success "Overlay applied"

    if [[ "${server_existed}" == "true" ]]; then
        log_info "Existing deployment detected — forcing a rollout restart so the freshly pushed :latest images are actually picked up (an unchanged Deployment spec does not trigger a new rollout on its own)..."
        kubectl rollout restart deployment -n "${NAMESPACE}" tmi-server
        kubectl rollout restart deployment -n "${NAMESPACE}" tmi-component-controller
    fi
}

# ============================================================================
# PHASE 6: Server CNAME
# ============================================================================

upsert_cname() {
    log_step "Phase 6: Server DNS (CNAME)"

    log_info "Waiting for ALB hostname..."
    local alb_host=""
    for _ in $(seq 1 60); do
        alb_host=$(kubectl get ingress tmi-server -n "${NAMESPACE}" \
            -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
        [[ -n "${alb_host}" ]] && break
        sleep 10
    done
    if [[ -z "${alb_host}" ]]; then
        log_error "Ingress never got an ALB hostname"
        exit 1
    fi
    log_success "ALB hostname: ${alb_host}"

    log_info "Upserting Route 53 CNAME ${DOMAIN} -> ${alb_host}..."
    aws route53 change-resource-record-sets --hosted-zone-id "${ZONE_ID}" \
        --change-batch "{\"Changes\":[{\"Action\":\"UPSERT\",\"ResourceRecordSet\":{
          \"Name\":\"${DOMAIN}\",\"Type\":\"CNAME\",\"TTL\":300,
          \"ResourceRecords\":[{\"Value\":\"${alb_host}\"}]}}]}" >/dev/null
    log_success "DNS record upserted"
}

# ============================================================================
# URL encoding helper (used by import_config())
# ============================================================================

# Percent-encode a string for safe use in a URL userinfo (username/password)
# position, mirroring terraform's urlencode() usage in
# terraform/modules/kubernetes/aws/k8s_resources.tf:99 (TMI_DATABASE_URL).
# random_password's override_special charset includes `% # ? & =`: a stray
# `%` makes Go's net/url.Parse hard-fail, and `#`/`?` truncate the URL at the
# fragment/query boundary before the real host:port/dbname is even reached.
# RFC 3986 unreserved characters (ALPHA / DIGIT / "-" / "." / "_" / "~") pass
# through unencoded; everything else becomes %XX. This is a superset of what
# userinfo strictly requires — safe, just occasionally over-encodes — and is
# pure bash (no external dependency, no shellout per character).
urlencode() {
    local string="$1" length i c
    length="${#string}"
    for (( i = 0; i < length; i++ )); do
        c="${string:i:1}"
        case "${c}" in
            [a-zA-Z0-9.~_-]) printf '%s' "${c}" ;;
            *) printf '%%%02X' "'${c}" ;;
        esac
    done
}

# ============================================================================
# PHASE 7: Optional dbtool config import (via in-cluster TCP proxy)
# ============================================================================

# RDS lives in private subnets and is unreachable directly from the deployer.
# We stand up a short-lived in-cluster socat TCP proxy (rds-proxy pod) and
# `kubectl port-forward` to it, then point dbtool's --config at a temporary,
# umask-077 YAML file whose database.url targets localhost:15432 with
# credentials fetched fresh from AWS Secrets Manager. Nothing here is ever
# echoed or logged, and `set -x` is never enabled anywhere in this script.
#
# Deviation from "make targets only": scripts/run-dbtool.py is hardcoded to
# tmi-dbtool's -t/--import-test-data (CATS seed) mode — it always builds and
# runs with -t plus --user/--provider/--server/--input-file, and cannot
# target an arbitrary host:port or drive --import-config. So this step
# builds the dbtool binary via the same command the `make build-dbtool`
# target uses (`uv run scripts/build-server.py --component dbtool`) and then
# invokes the built ./bin/tmi-dbtool binary directly with --import-config,
# since no existing wrapper or make target exposes that operation.
import_config() {
    if [[ -z "${CONFIG_EXPORT_FILE}" ]]; then
        return 0
    fi

    log_step "Phase 7: Config Import"

    if [[ ! -f "${CONFIG_EXPORT_FILE}" ]]; then
        log_error "--config-export file not found: ${CONFIG_EXPORT_FILE}"
        exit 1
    fi

    log_info "Starting in-cluster RDS proxy..."
    kubectl run rds-proxy --restart=Never -n "${NAMESPACE}" \
        --image=alpine/socat -- tcp-listen:5432,fork,reuseaddr "tcp:${RDS_ENDPOINT}:5432"
    RDS_PROXY_STARTED=true
    kubectl wait --for=condition=Ready pod/rds-proxy -n "${NAMESPACE}" --timeout=120s

    kubectl port-forward -n "${NAMESPACE}" pod/rds-proxy 15432:5432 &
    PF_PID=$!
    sleep 2 # give the port-forward a moment to establish before connecting

    log_info "Fetching database credentials from Secrets Manager (not logged)..."
    local secret_json db_user db_password
    secret_json=$(aws secretsmanager get-secret-value \
        --secret-id "${NAME_PREFIX}-db-credentials" --region "${REGION}" \
        --query SecretString --output text)
    db_user=$(echo "${secret_json}" | jq -r '.username')
    db_password=$(echo "${secret_json}" | jq -r '.password')
    unset secret_json

    local db_password_encoded
    db_password_encoded=$(urlencode "${db_password}")
    unset db_password

    local tmp_dir
    tmp_dir=$(mktemp -d)
    TMPDIR_TO_CLEAN="${tmp_dir}"
    ( umask 077
      cat > "${tmp_dir}/dbtool-connect.yaml" <<EOF
server:
  port: "8080"
  interface: "0.0.0.0"
database:
  url: "postgres://${db_user}:${db_password_encoded}@localhost:15432/tmi?sslmode=require"
  redis:
    host: "localhost"
    port: "6379"
auth:
  build_mode: "test"
  jwt:
    secret: "deploy-aws-transient-connect-only"
EOF
    )
    unset db_password_encoded

    log_info "Building dbtool..."
    (cd "${PROJECT_ROOT}" && uv run "${SCRIPT_DIR}/build-server.py" --component dbtool)

    log_info "Importing config from ${CONFIG_EXPORT_FILE}..."
    "${PROJECT_ROOT}/bin/tmi-dbtool" --import-config -f "${CONFIG_EXPORT_FILE}" \
        --config "${tmp_dir}/dbtool-connect.yaml" --overwrite

    log_success "Config import complete"

    rm -rf "${tmp_dir}"
    TMPDIR_TO_CLEAN=""
    kill "${PF_PID}" 2>/dev/null || true
    PF_PID=""
    kubectl delete pod rds-proxy -n "${NAMESPACE}" --ignore-not-found >/dev/null 2>&1 || true
    RDS_PROXY_STARTED=false
}

# ============================================================================
# PHASE 8: Verification
# ============================================================================

verify_deployment() {
    log_step "Phase 8: Verification"

    local code="000"
    for _ in $(seq 1 30); do
        code=$(curl -s -o /dev/null -w '%{http_code}' "https://${DOMAIN}/" || true)
        [[ "${code}" == "200" ]] && break
        sleep 10
    done
    if [[ "${code}" != "200" ]]; then
        log_error "https://${DOMAIN}/ returned ${code}"
        exit 1
    fi
    log_success "https://${DOMAIN}/ is responding (HTTP 200)"

    curl -s "https://${DOMAIN}/" | jq . || true
    kubectl get pods -n "${NAMESPACE}"

    echo ""
    log_step "Deployment Summary"
    echo "  Region:          ${REGION}"
    echo "  Name prefix:     ${NAME_PREFIX}"
    echo "  AWS profile:     ${PROFILE}"
    echo "  EKS cluster:     ${CLUSTER_NAME}"
    echo "  Domain:          https://${DOMAIN}"
    echo ""
    echo "  Useful commands:"
    echo "    kubectl get pods -n ${NAMESPACE}                 # Check pod status"
    echo "    kubectl logs -n ${NAMESPACE} -l app=tmi-server   # View API logs"
    echo "    ./scripts/deploy-aws.sh --destroy                # Tear down"
    echo ""
    log_warning "Auth posture: the tmi stub provider is disabled and first-user auto-promotion"
    log_warning "is OFF. Admin access comes solely from the 'administrators' setting in the"
    log_warning "replicated database config (expected: the configured Google admin identity)."
    log_warning "Every authenticated user is a security reviewer (everyone_is_a_reviewer=true)."
    log_warning "If no administrator is seeded, NO ONE will have admin — verify the imported config."
}

# ============================================================================
# Main
# ============================================================================

main() {
    echo -e "${BOLD}TMI AWS Deployment${NC}"
    echo ""

    parse_args "$@"

    preflight_checks
    terraform_deploy

    if [[ "${DESTROY}" == "true" ]] || [[ "${DRY_RUN}" == "true" ]]; then
        # terraform_deploy() already logged plan/destroy-plan output above.
        # Nothing is built/pushed and the cluster is never touched.
        return 0
    fi

    capture_terraform_outputs
    build_and_push_images
    configure_kubeconfig
    apply_platform_base
    create_embedding_secret
    apply_overlay
    upsert_cname
    import_config
    verify_deployment

    log_success "Deployment complete!"
}

main "$@"
