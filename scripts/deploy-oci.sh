#!/bin/bash
# scripts/deploy-oci.sh
#
# Deploy TMI to OCI using Terraform and OKE.
# Handles the two-phase deployment required because the Kubernetes provider
# cannot initialize until the OKE cluster exists and kubeconfig is generated.
#
# Phase 1: Create OCI infrastructure (network, database, secrets, OKE cluster)
# Phase 1.5: Build and push container images to OCIR
# Phase 2: Generate kubeconfig, then create Kubernetes resources (deployments, services)
#
# Usage: ./scripts/deploy-oci.sh [options]
#
# Options:
#   --environment ENV    Terraform environment (default: oci-public)
#   --profile PROFILE    OCI CLI config profile (read from terraform.tfvars if not set)
#   --region REGION       OCI region (read from terraform.tfvars if not set)
#   --destroy            Destroy the deployment instead of creating it
#   --dry-run            Run terraform plan only (no apply)
#   --auto-approve       Skip terraform apply confirmation
#   --skip-build         Skip container build+push (use existing images in OCIR)
#   --push-info          Print OCIR push instructions for external containers and exit
#   --push-env           Output OCIR registry info as shell env vars (eval-able)
#   --help               Show this help message
#
# Examples:
#   ./scripts/deploy-oci.sh
#   ./scripts/deploy-oci.sh --environment oci-private --profile tmi
#   ./scripts/deploy-oci.sh --dry-run
#   ./scripts/deploy-oci.sh --destroy
#   ./scripts/deploy-oci.sh --skip-build
#   ./scripts/deploy-oci.sh --push-info

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
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Defaults
TF_ENV="oci-public"
OCI_PROFILE=""
OCI_REGION=""
DESTROY=false
DRY_RUN=false
AUTO_APPROVE=false
SKIP_BUILD=false
PUSH_INFO=false
PUSH_ENV=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --environment)
            TF_ENV="$2"
            shift 2
            ;;
        --profile)
            OCI_PROFILE="$2"
            shift 2
            ;;
        --region)
            OCI_REGION="$2"
            shift 2
            ;;
        --destroy)
            DESTROY=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --auto-approve)
            AUTO_APPROVE=true
            shift
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        --push-info)
            PUSH_INFO=true
            shift
            ;;
        --push-env)
            PUSH_ENV=true
            shift
            ;;
        --help)
            head -35 "$0" | tail -33
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

TF_DIR="$PROJECT_ROOT/terraform/environments/$TF_ENV"

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------
preflight_checks() {
    log_step "Preflight Checks"

    if [[ ! -d "$TF_DIR" ]]; then
        log_error "Terraform environment directory not found: $TF_DIR"
        exit 1
    fi

    local missing=()
    command -v terraform >/dev/null 2>&1 || missing+=("terraform")
    command -v oci >/dev/null 2>&1 || missing+=("oci (OCI CLI)")
    command -v kubectl >/dev/null 2>&1 || missing+=("kubectl")
    command -v jq >/dev/null 2>&1 || missing+=("jq")

    if [[ ${#missing[@]} -gt 0 ]]; then
        log_error "Missing required tools: ${missing[*]}"
        log_info "Install with: brew install ${missing[*]}"
        exit 1
    fi

    # Read profile and region from terraform.tfvars if not provided
    if [[ -z "$OCI_PROFILE" ]]; then
        OCI_PROFILE=$(grep '^oci_config_profile' "$TF_DIR/terraform.tfvars" 2>/dev/null \
            | sed 's/.*= *"//;s/".*//' || echo "DEFAULT")
    fi
    if [[ -z "$OCI_REGION" ]]; then
        OCI_REGION=$(grep '^region' "$TF_DIR/terraform.tfvars" 2>/dev/null \
            | sed 's/.*= *"//;s/".*//' || echo "us-ashburn-1")
    fi

    # Verify OCI CLI authentication
    if ! oci iam region list --profile "$OCI_PROFILE" --output json >/dev/null 2>&1; then
        log_error "OCI CLI authentication failed for profile '$OCI_PROFILE'"
        log_info "Run: oci setup config --profile $OCI_PROFILE"
        exit 1
    fi

    log_success "All preflight checks passed (profile=$OCI_PROFILE, region=$OCI_REGION)"
}

# ---------------------------------------------------------------------------
# Terraform init
# ---------------------------------------------------------------------------
tf_init() {
    log_info "Initializing Terraform in $TF_DIR..."
    cd "$TF_DIR"
    terraform init -input=false
    log_success "Terraform initialized"
}

# ---------------------------------------------------------------------------
# Get cluster ID from Terraform state (reads resource attribute directly)
# ---------------------------------------------------------------------------
get_cluster_id_from_state() {
    cd "$TF_DIR"
    terraform state show module.kubernetes.oci_containerengine_cluster.tmi 2>/dev/null \
        | grep '^\s*id\s*=' | head -1 | sed 's/.*= *"//;s/".*//' || echo ""
}

# ---------------------------------------------------------------------------
# Generate kubeconfig for OKE cluster
# ---------------------------------------------------------------------------
generate_kubeconfig() {
    local cluster_id="$1"
    if [[ -z "$cluster_id" ]]; then
        log_error "Cannot generate kubeconfig: no cluster ID"
        return 1
    fi

    log_info "Generating kubeconfig for cluster $cluster_id..."
    oci ce cluster create-kubeconfig \
        --cluster-id "$cluster_id" \
        --region "$OCI_REGION" \
        --profile "$OCI_PROFILE" \
        --token-version 2.0.0 \
        --overwrite
    log_success "Kubeconfig generated"
}

# ---------------------------------------------------------------------------
# Create a minimal empty kubeconfig (valid YAML, no clusters)
# Used during Phase 1 so the kubernetes provider can initialize without
# a real cluster endpoint.
# ---------------------------------------------------------------------------
create_empty_kubeconfig() {
    local tmpfile
    tmpfile=$(mktemp /tmp/tmi-empty-kubeconfig.XXXXXX)
    cat > "$tmpfile" <<'KUBECONFIG'
apiVersion: v1
kind: Config
clusters: []
contexts: []
current-context: ""
users: []
KUBECONFIG
    echo "$tmpfile"
}

# ---------------------------------------------------------------------------
# Wait for OKE cluster to be ACTIVE
# ---------------------------------------------------------------------------
wait_for_cluster() {
    local cluster_id="$1"
    local timeout=600
    local interval=15
    local elapsed=0

    log_info "Waiting for OKE cluster to become ACTIVE (timeout: ${timeout}s)..."
    while [[ $elapsed -lt $timeout ]]; do
        local state
        state=$(oci ce cluster get \
            --cluster-id "$cluster_id" \
            --profile "$OCI_PROFILE" \
            --query 'data."lifecycle-state"' \
            --raw-output 2>/dev/null || echo "UNKNOWN")

        if [[ "$state" == "ACTIVE" ]]; then
            log_success "OKE cluster is ACTIVE"
            return 0
        fi

        echo -e "  Cluster state: $state (${elapsed}s elapsed)"
        sleep "$interval"
        elapsed=$((elapsed + interval))
    done

    log_error "Timed out waiting for cluster to become ACTIVE"
    return 1
}

# ---------------------------------------------------------------------------
# Wait for node pool nodes to be ready
# ---------------------------------------------------------------------------
wait_for_nodes() {
    local timeout=600
    local interval=20
    local elapsed=0

    log_info "Waiting for Kubernetes nodes to be Ready (timeout: ${timeout}s)..."
    while [[ $elapsed -lt $timeout ]]; do
        local ready_nodes
        ready_nodes=$(kubectl get nodes --no-headers 2>/dev/null \
            | grep -c " Ready" || echo "0")

        if [[ "$ready_nodes" -gt 0 ]]; then
            log_success "$ready_nodes node(s) Ready"
            return 0
        fi

        echo -e "  $ready_nodes nodes ready (${elapsed}s elapsed)"
        sleep "$interval"
        elapsed=$((elapsed + interval))
    done

    log_warning "Timed out waiting for nodes — continuing anyway (K8s resources may fail)"
    return 0
}

# ---------------------------------------------------------------------------
# Build and push container images to OCIR
# ---------------------------------------------------------------------------
build_and_push_containers() {
    log_step "Building and Pushing Containers"

    log_info "Building TMI server and Redis containers (arm64 for OKE)..."
    OCI_CLI_PROFILE="$OCI_PROFILE" OCI_REGION="$OCI_REGION" \
        uv run "$PROJECT_ROOT/scripts/build-app-containers.py" \
            --target oci \
            --component all \
            --push

    log_success "Containers built and pushed to OCIR"

    # Print instructions for external containers if enabled
    print_external_container_info
}

# ---------------------------------------------------------------------------
# Print OCIR push instructions for external containers (tmi-ux, tmi-tf-wh)
# ---------------------------------------------------------------------------
print_external_container_info() {
    local namespace
    namespace=$(oci os ns get --query data --raw-output --profile "$OCI_PROFILE" 2>/dev/null || echo "")
    local registry="${OCI_REGION}.ocir.io"
    local name_prefix
    name_prefix=$(grep '^name_prefix' "$TF_DIR/terraform.tfvars" 2>/dev/null \
        | sed 's/.*= *"//;s/".*//' || echo "tmi")

    local tmi_ux_enabled
    tmi_ux_enabled=$(grep '^tmi_ux_enabled' "$TF_DIR/terraform.tfvars" 2>/dev/null \
        | sed 's/.*= *//' | tr -d ' ' || echo "false")
    local tmi_tf_wh_enabled
    tmi_tf_wh_enabled=$(grep '^tmi_tf_wh_enabled' "$TF_DIR/terraform.tfvars" 2>/dev/null \
        | sed 's/.*= *//' | tr -d ' ' || echo "false")

    local has_external=false

    if [[ "$tmi_ux_enabled" == "true" ]]; then
        has_external=true
        echo ""
        log_info "TMI-UX is enabled. Push the tmi-ux container image to:"
        echo -e "  ${BOLD}${registry}/${namespace}/${name_prefix}/tmi-ux:latest${NC}"
        echo -e "  From the tmi-ux repo: docker buildx build --platform linux/arm64 --push -t ${registry}/${namespace}/${name_prefix}/tmi-ux:latest ."
    fi

    if [[ "$tmi_tf_wh_enabled" == "true" ]]; then
        has_external=true
        echo ""
        log_info "tmi-tf-wh is enabled. Push the tmi-tf-wh container image to:"
        echo -e "  ${BOLD}${registry}/${namespace}/${name_prefix}/tmi-tf-wh:latest${NC}"
        echo -e "  From the tmi-tf-wh repo: docker buildx build --platform linux/arm64 --push -t ${registry}/${namespace}/${name_prefix}/tmi-tf-wh:latest ."
    fi

    if $has_external; then
        echo ""
        log_info "OCIR login (if not already authenticated):"
        echo -e "  docker login ${registry} -u ${namespace}/<your-oci-username>"
        echo -e "  (Use an OCI Auth Token as password — create at OCI Console > User Settings > Auth Tokens)"
        echo ""
        log_info "Or use eval to set env vars for external build scripts:"
        echo -e "  eval \$(scripts/deploy-oci.sh --push-env)"
    fi
}

# ---------------------------------------------------------------------------
# Output OCIR registry info as shell env vars (machine-readable)
# Usage from external projects: eval $(path/to/deploy-oci.sh --push-env)
# ---------------------------------------------------------------------------
print_push_env() {
    local namespace
    namespace=$(oci os ns get --query data --raw-output --profile "$OCI_PROFILE" 2>/dev/null || echo "")
    local registry="${OCI_REGION}.ocir.io"
    local name_prefix
    name_prefix=$(grep '^name_prefix' "$TF_DIR/terraform.tfvars" 2>/dev/null \
        | sed 's/.*= *"//;s/".*//' || echo "tmi")

    # Common registry info
    echo "export OCIR_REGISTRY=${registry}"
    echo "export OCIR_NAMESPACE=${namespace}"
    echo "export OCIR_REGION=${OCI_REGION}"
    echo "export OCIR_BASE_URL=${registry}/${namespace}/${name_prefix}"
    echo "export OCIR_PLATFORM=linux/arm64"

    # Per-component image URLs
    echo "export TMI_IMAGE_URL=${registry}/${namespace}/${name_prefix}/tmi:latest"
    echo "export TMI_REDIS_IMAGE_URL=${registry}/${namespace}/${name_prefix}/tmi-redis:latest"
    echo "export TMI_UX_IMAGE_URL=${registry}/${namespace}/${name_prefix}/tmi-ux:latest"
    echo "export TMI_TF_WH_IMAGE_URL=${registry}/${namespace}/${name_prefix}/tmi-tf-wh:latest"
}

# ---------------------------------------------------------------------------
# Delete orphaned OCI load balancers
# ---------------------------------------------------------------------------
cleanup_load_balancers() {
    local compartment_id
    compartment_id=$(grep '^compartment_id' "$TF_DIR/terraform.tfvars" 2>/dev/null \
        | sed 's/.*= *"//;s/".*//')

    if [[ -z "$compartment_id" ]]; then
        log_warning "Cannot determine compartment_id — skipping LB cleanup"
        return 0
    fi

    local lb_ids
    lb_ids=$(oci lb load-balancer list \
        --compartment-id "$compartment_id" \
        --profile "$OCI_PROFILE" \
        --query 'data[*].id' \
        --raw-output 2>/dev/null | jq -r '.[]' 2>/dev/null || echo "")

    if [[ -z "$lb_ids" ]]; then
        log_info "No orphaned load balancers found"
        return 0
    fi

    local count
    count=$(echo "$lb_ids" | wc -l | tr -d ' ')
    log_warning "Found $count load balancer(s) that may block subnet/NSG deletion"

    for lb_id in $lb_ids; do
        log_info "Deleting load balancer: $lb_id"
        oci lb load-balancer delete \
            --load-balancer-id "$lb_id" \
            --profile "$OCI_PROFILE" \
            --force 2>/dev/null || true
    done

    # Wait for deletions to complete
    log_info "Waiting for load balancer deletions to complete..."
    local timeout=300
    local elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        local remaining
        remaining=$(oci lb load-balancer list \
            --compartment-id "$compartment_id" \
            --profile "$OCI_PROFILE" \
            --query 'data | length(@)' \
            --raw-output 2>/dev/null || echo "0")

        if [[ "$remaining" == "0" ]]; then
            log_success "All load balancers deleted"
            return 0
        fi

        echo -e "  $remaining LB(s) still deleting (${elapsed}s elapsed)"
        sleep 10
        elapsed=$((elapsed + 10))
    done

    log_warning "Timed out waiting for LB cleanup — destroy may fail on subnet/NSG"
}

# ---------------------------------------------------------------------------
# Remove kubernetes resources from state (workaround for provider v3.x bug)
# ---------------------------------------------------------------------------
remove_k8s_from_state() {
    cd "$TF_DIR"
    local k8s_resources
    k8s_resources=$(terraform state list 2>/dev/null | grep "kubernetes_" || echo "")

    if [[ -z "$k8s_resources" ]]; then
        return 0
    fi

    log_info "Removing Kubernetes resources from state (provider v3.x identity bug workaround)..."
    while IFS= read -r resource; do
        terraform state rm "$resource" >/dev/null 2>&1 || true
    done <<< "$k8s_resources"
    log_success "Kubernetes resources removed from state"
}

# ---------------------------------------------------------------------------
# Build terraform apply/destroy args
# ---------------------------------------------------------------------------
tf_approve_arg() {
    if $AUTO_APPROVE; then
        echo "-auto-approve"
    fi
}

# ---------------------------------------------------------------------------
# Destroy
# ---------------------------------------------------------------------------
do_destroy() {
    log_step "Destroying OCI Infrastructure"

    cd "$TF_DIR"
    tf_init

    if $DRY_RUN; then
        log_info "Dry run: showing destroy plan..."
        GODEBUG=x509negativeserial=1 terraform plan -destroy
        return
    fi

    # Phase 1: Clean up K8s resources from state and delete orphan LBs
    remove_k8s_from_state
    cleanup_load_balancers

    # Phase 2: Destroy remaining OCI infrastructure
    # Use an empty kubeconfig so the provider doesn't try to connect
    local empty_kubeconfig
    empty_kubeconfig=$(create_empty_kubeconfig)
    trap "rm -f '$empty_kubeconfig'" EXIT

    log_info "Destroying OCI infrastructure..."
    GODEBUG=x509negativeserial=1 terraform destroy \
        -var "kubeconfig_path=$empty_kubeconfig" \
        $(tf_approve_arg)

    rm -f "$empty_kubeconfig"
    log_success "Infrastructure destroyed"
}

# ---------------------------------------------------------------------------
# Deploy
# ---------------------------------------------------------------------------
do_deploy() {
    log_step "Deploying TMI to OCI ($TF_ENV)"

    cd "$TF_DIR"
    tf_init

    # Check if the OKE cluster already exists in state
    local cluster_id
    cluster_id=$(get_cluster_id_from_state)

    if [[ -n "$cluster_id" ]]; then
        # Cluster exists — generate kubeconfig and do a single apply
        log_info "OKE cluster exists ($cluster_id), running full apply..."
        generate_kubeconfig "$cluster_id"

        if $DRY_RUN; then
            GODEBUG=x509negativeserial=1 terraform plan
            return
        fi

        # Build containers unless skipped
        if ! $SKIP_BUILD; then
            build_and_push_containers
        fi

        GODEBUG=x509negativeserial=1 terraform apply $(tf_approve_arg)
    else
        # Cluster does not exist — two-phase deploy
        log_step "Phase 1: OCI Infrastructure"
        log_info "Creating OCI resources (network, database, secrets, OKE cluster)..."
        log_info "Kubernetes resources will be created in Phase 2 after the cluster is ready."

        if $DRY_RUN; then
            GODEBUG=x509negativeserial=1 terraform plan
            return
        fi

        # Phase 1: Apply with an empty kubeconfig so the kubernetes provider
        # initializes without trying to connect to a cluster. OCI resources
        # (network, DB, secrets, OKE cluster, node pool) are created.
        # K8s resources (namespace, deployments, services) will fail — expected.
        local empty_kubeconfig
        empty_kubeconfig=$(create_empty_kubeconfig)
        trap "rm -f '$empty_kubeconfig'" EXIT

        log_info "Phase 1 apply (K8s resource errors are expected and will be resolved in Phase 2)..."
        GODEBUG=x509negativeserial=1 terraform apply \
            -var "kubeconfig_path=$empty_kubeconfig" \
            $(tf_approve_arg) \
            2>&1 | tee /tmp/tmi-deploy-phase1.log || true

        rm -f "$empty_kubeconfig"

        # Get the cluster ID from the new state
        cluster_id=$(get_cluster_id_from_state)
        if [[ -z "$cluster_id" ]]; then
            log_error "Phase 1 failed: OKE cluster was not created"
            log_info "Check the output above and /tmp/tmi-deploy-phase1.log for errors"
            exit 1
        fi

        log_success "Phase 1 complete: OKE cluster created ($cluster_id)"

        # Wait for cluster to become ACTIVE
        wait_for_cluster "$cluster_id"

        # Build and push containers while waiting for nodes
        if ! $SKIP_BUILD; then
            build_and_push_containers
        fi

        # Generate kubeconfig now that the cluster exists
        log_step "Phase 2: Kubernetes Resources"
        generate_kubeconfig "$cluster_id"

        # Wait for nodes to be schedulable
        wait_for_nodes

        # Phase 2: Full apply — now the kubernetes provider can connect
        log_info "Phase 2 apply (creating Kubernetes resources)..."
        GODEBUG=x509negativeserial=1 terraform apply $(tf_approve_arg)
    fi

    log_step "Deployment Complete"

    # Show key outputs
    local lb_ip
    lb_ip=$(terraform output -raw load_balancer_ip 2>/dev/null || echo "<pending>")
    local kubeconfig_cmd
    kubeconfig_cmd=$(terraform output -raw kubernetes_config_command 2>/dev/null || echo "")

    echo ""
    log_success "TMI deployed to OCI"
    echo ""
    echo -e "  ${BOLD}TMI API:${NC}          http://$lb_ip/"
    echo -e "  ${BOLD}Kubeconfig:${NC}       $kubeconfig_cmd"
    echo -e "  ${BOLD}Check pods:${NC}       kubectl get pods -n tmi"
    echo ""
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

# Handle --push-info and --push-env without full preflight
if $PUSH_INFO || $PUSH_ENV; then
    # Minimal setup: read profile/region from tfvars
    if [[ -z "$OCI_PROFILE" ]]; then
        OCI_PROFILE=$(grep '^oci_config_profile' "$TF_DIR/terraform.tfvars" 2>/dev/null \
            | sed 's/.*= *"//;s/".*//' || echo "DEFAULT")
    fi
    if [[ -z "$OCI_REGION" ]]; then
        OCI_REGION=$(grep '^region' "$TF_DIR/terraform.tfvars" 2>/dev/null \
            | sed 's/.*= *"//;s/".*//' || echo "us-ashburn-1")
    fi
    if $PUSH_ENV; then
        print_push_env
    else
        print_external_container_info
    fi
    exit 0
fi

preflight_checks

if $DESTROY; then
    do_destroy
else
    do_deploy
fi
