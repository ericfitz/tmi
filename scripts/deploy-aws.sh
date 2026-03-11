#!/bin/bash
# scripts/deploy-aws.sh
#
# Deploy TMI to AWS using Terraform and EKS.
# Performs preflight checks, builds/pushes container images to ECR,
# generates a temporary terraform.tfvars (gitignored), deploys, and verifies.
#
# Usage: ./scripts/deploy-aws.sh [options]
#
# Options:
#   --region REGION              AWS region (default: us-east-1)
#   --name-prefix PREFIX         Resource name prefix (default: tmi)
#   --domain DOMAIN              Domain name for ACM certificate (enables HTTPS)
#   --zone-id ZONE_ID            Route 53 hosted zone ID (required with --domain)
#   --san NAMES                  Comma-separated subject alternative names
#   --alert-email EMAIL          Email for CloudWatch alarm notifications
#   --db-instance-class CLASS    RDS instance class (default: db.t4g.micro)
#   --db-multi-az                Enable Multi-AZ for RDS
#   --multi-az-nat               Enable NAT Gateway per AZ (HA, higher cost)
#   --skip-build                 Skip container image build (use existing ECR images)
#   --destroy                    Destroy the deployment instead of creating it
#   --dry-run                    Run terraform plan only (no apply)
#   --auto-approve               Skip terraform apply confirmation
#   --help                       Show this help message
#
# Examples:
#   ./scripts/deploy-aws.sh
#   ./scripts/deploy-aws.sh --region us-west-2 --domain tmi.example.com --zone-id Z1234567890ABC
#   ./scripts/deploy-aws.sh --skip-build --dry-run
#   ./scripts/deploy-aws.sh --destroy

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
TF_DIR="${PROJECT_ROOT}/terraform/environments/aws-production"

# Default values
REGION="us-east-1"
NAME_PREFIX="tmi"
DOMAIN=""
ZONE_ID=""
SAN=""
ALERT_EMAIL=""
DB_INSTANCE_CLASS="db.t4g.micro"
DB_MULTI_AZ=false
MULTI_AZ_NAT=false
SKIP_BUILD=false
DESTROY=false
DRY_RUN=false
AUTO_APPROVE=false

# Container image names (must match build-containers.sh)
LOCAL_TMI_IMAGE="tmi/tmi-server:latest"
LOCAL_REDIS_IMAGE="tmi/tmi-redis:latest"

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --region)
                REGION="$2"; shift 2 ;;
            --name-prefix)
                NAME_PREFIX="$2"; shift 2 ;;
            --domain)
                DOMAIN="$2"; shift 2 ;;
            --zone-id)
                ZONE_ID="$2"; shift 2 ;;
            --san)
                SAN="$2"; shift 2 ;;
            --alert-email)
                ALERT_EMAIL="$2"; shift 2 ;;
            --db-instance-class)
                DB_INSTANCE_CLASS="$2"; shift 2 ;;
            --db-multi-az)
                DB_MULTI_AZ=true; shift ;;
            --multi-az-nat)
                MULTI_AZ_NAT=true; shift ;;
            --skip-build)
                SKIP_BUILD=true; shift ;;
            --destroy)
                DESTROY=true; shift ;;
            --dry-run)
                DRY_RUN=true; shift ;;
            --auto-approve)
                AUTO_APPROVE=true; shift ;;
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
    head -28 "${BASH_SOURCE[0]}" | tail -26
}

# ============================================================================
# PHASE 1: Preflight Checks
# ============================================================================

preflight_checks() {
    log_step "Phase 1: Preflight Checks"

    local failed=0

    # 1. AWS CLI
    if command -v aws &>/dev/null; then
        local aws_version
        aws_version=$(aws --version 2>&1 | awk '{print $1}')
        log_success "AWS CLI installed (${aws_version})"
    else
        log_error "AWS CLI not installed"
        echo "  Install: brew install awscli"
        echo "  Docs:    https://docs.aws.amazon.com/cli/latest/userguide/install-cliv2.html"
        failed=1
    fi

    # 2. AWS credentials
    if aws sts get-caller-identity &>/dev/null; then
        local account_id identity
        account_id=$(aws sts get-caller-identity --query Account --output text)
        identity=$(aws sts get-caller-identity --query Arn --output text)
        log_success "AWS credentials valid (account: ${account_id}, identity: ${identity})"
        ACCOUNT_ID="${account_id}"
    else
        log_error "AWS credentials not configured or expired"
        echo "  Configure: aws configure"
        echo "  Or set:    AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables"
        echo "  For SSO:   aws sso login --profile your-profile"
        failed=1
    fi

    # 3. Terraform
    if command -v terraform &>/dev/null; then
        local tf_version
        tf_version=$(terraform version -json 2>/dev/null | jq -r '.terraform_version' 2>/dev/null || terraform version | head -1)
        log_success "Terraform installed (${tf_version})"

        # Check minimum version
        local major minor
        major=$(echo "${tf_version}" | cut -d. -f1)
        minor=$(echo "${tf_version}" | cut -d. -f2)
        if [[ "${major}" -lt 1 ]] || { [[ "${major}" -eq 1 ]] && [[ "${minor}" -lt 5 ]]; }; then
            log_error "Terraform >= 1.5.0 required (found ${tf_version})"
            echo "  Upgrade: brew upgrade terraform"
            failed=1
        fi
    else
        log_error "Terraform not installed"
        echo "  Install: brew install terraform"
        failed=1
    fi

    # 4. Docker (unless skipping build)
    if [[ "${SKIP_BUILD}" == "false" ]]; then
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
            echo "  Install: brew install --cask docker"
            echo "  Or skip:  --skip-build (if images are already in ECR)"
            failed=1
        fi
    else
        log_info "Skipping Docker check (--skip-build)"
    fi

    # 5. kubectl
    if command -v kubectl &>/dev/null; then
        log_success "kubectl installed"
    else
        log_warning "kubectl not installed (needed for post-deploy verification)"
        echo "  Install: brew install kubectl"
    fi

    # 6. jq
    if command -v jq &>/dev/null; then
        log_success "jq installed"
    else
        log_error "jq not installed (required for script)"
        echo "  Install: brew install jq"
        failed=1
    fi

    # 7. Terraform directory exists
    if [[ -d "${TF_DIR}" ]]; then
        log_success "Terraform config found at terraform/environments/aws-production/"
    else
        log_error "Terraform config not found at ${TF_DIR}"
        echo "  Ensure you are running from the TMI project root"
        failed=1
    fi

    # 8. Validate certificate arguments
    if [[ -n "${DOMAIN}" ]] && [[ -z "${ZONE_ID}" ]]; then
        log_warning "--domain specified without --zone-id"
        echo "  Certificate will require manual DNS validation"
        echo "  For automatic validation, provide: --zone-id <Route53-zone-id>"
    fi

    # 9. Check AWS region is valid
    if [[ "${failed}" -eq 0 ]]; then
        if aws ec2 describe-regions --region-names "${REGION}" &>/dev/null; then
            log_success "AWS region '${REGION}' is valid"
        else
            log_error "Invalid AWS region: ${REGION}"
            echo "  List regions: aws ec2 describe-regions --query 'Regions[].RegionName' --output text"
            failed=1
        fi
    fi

    # 10. Check for existing local images (unless skipping build)
    if [[ "${SKIP_BUILD}" == "false" ]] && [[ "${DESTROY}" == "false" ]]; then
        if docker image inspect "${LOCAL_TMI_IMAGE}" &>/dev/null; then
            log_success "Local TMI image found: ${LOCAL_TMI_IMAGE}"
        else
            log_info "Local TMI image not found -- will build with 'make build-container-tmi'"
        fi
        if docker image inspect "${LOCAL_REDIS_IMAGE}" &>/dev/null; then
            log_success "Local Redis image found: ${LOCAL_REDIS_IMAGE}"
        else
            log_info "Local Redis image not found -- will build with 'make build-container-redis'"
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
# PHASE 2: Container Images
# ============================================================================

setup_ecr_and_push() {
    if [[ "${DESTROY}" == "true" ]]; then
        return 0
    fi

    log_step "Phase 2: Container Images"

    ECR_REGISTRY="${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com"
    ECR_TMI_IMAGE="${ECR_REGISTRY}/${NAME_PREFIX}-server:latest"
    ECR_REDIS_IMAGE="${ECR_REGISTRY}/${NAME_PREFIX}-redis:latest"

    # Create ECR repositories if they don't exist
    for repo in "${NAME_PREFIX}-server" "${NAME_PREFIX}-redis"; do
        if aws ecr describe-repositories --repository-names "${repo}" --region "${REGION}" &>/dev/null; then
            log_success "ECR repository exists: ${repo}"
        else
            log_info "Creating ECR repository: ${repo}"
            aws ecr create-repository \
                --repository-name "${repo}" \
                --region "${REGION}" \
                --image-scanning-configuration scanOnPush=true \
                --encryption-configuration encryptionType=AES256 \
                --output text --query 'repository.repositoryUri'
            log_success "Created ECR repository: ${repo}"
        fi
    done

    if [[ "${SKIP_BUILD}" == "true" ]]; then
        # Verify images exist in ECR
        log_info "Skipping build, verifying images exist in ECR..."
        local missing=0
        for repo in "${NAME_PREFIX}-server" "${NAME_PREFIX}-redis"; do
            if aws ecr describe-images --repository-name "${repo}" --image-ids imageTag=latest --region "${REGION}" &>/dev/null; then
                log_success "ECR image found: ${repo}:latest"
            else
                log_error "ECR image not found: ${repo}:latest"
                echo "  Either push the image or remove --skip-build to build locally"
                missing=1
            fi
        done
        if [[ "${missing}" -ne 0 ]]; then
            exit 1
        fi
        return 0
    fi

    # Build containers if needed
    if ! docker image inspect "${LOCAL_TMI_IMAGE}" &>/dev/null; then
        log_info "Building TMI server container..."
        (cd "${PROJECT_ROOT}" && make build-container-tmi)
    fi

    if ! docker image inspect "${LOCAL_REDIS_IMAGE}" &>/dev/null; then
        log_info "Building Redis container..."
        (cd "${PROJECT_ROOT}" && make build-container-redis)
    fi

    # Login to ECR
    log_info "Logging into ECR..."
    aws ecr get-login-password --region "${REGION}" | \
        docker login --username AWS --password-stdin "${ECR_REGISTRY}"
    log_success "ECR login successful"

    # Tag and push
    log_info "Pushing TMI server image to ECR..."
    docker tag "${LOCAL_TMI_IMAGE}" "${ECR_TMI_IMAGE}"
    docker push "${ECR_TMI_IMAGE}"
    log_success "Pushed: ${ECR_TMI_IMAGE}"

    log_info "Pushing Redis image to ECR..."
    docker tag "${LOCAL_REDIS_IMAGE}" "${ECR_REDIS_IMAGE}"
    docker push "${ECR_REDIS_IMAGE}"
    log_success "Pushed: ${ECR_REDIS_IMAGE}"
}

# ============================================================================
# PHASE 3: Terraform Deploy
# ============================================================================

terraform_deploy() {
    log_step "Phase 3: Terraform ${DESTROY:+Destroy}${DESTROY:-Deploy}"

    cd "${TF_DIR}"

    # Initialize Terraform
    log_info "Initializing Terraform..."
    terraform init -upgrade
    log_success "Terraform initialized"

    if [[ "${DESTROY}" == "true" ]]; then
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_info "Dry run: showing destroy plan..."
            terraform plan -destroy
        else
            log_warning "This will DESTROY all TMI AWS resources!"
            if [[ "${AUTO_APPROVE}" == "true" ]]; then
                terraform destroy -auto-approve
            else
                terraform destroy
            fi
            log_success "Infrastructure destroyed"
        fi
        return 0
    fi

    # Generate terraform.tfvars (gitignored, not persisted to repo)
    generate_tfvars

    # Plan
    log_info "Running terraform plan..."
    terraform plan -out=tfplan
    log_success "Plan complete"

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "Dry run complete. Review the plan above."
        log_info "To apply, run without --dry-run"
        cleanup_tfplan
        return 0
    fi

    # Apply
    log_info "Applying Terraform plan..."
    if [[ "${AUTO_APPROVE}" == "true" ]]; then
        terraform apply tfplan
    else
        echo ""
        echo -e "${YELLOW}Review the plan above. Terraform will prompt for confirmation.${NC}"
        echo ""
        terraform apply tfplan
    fi

    cleanup_tfplan
    log_success "Terraform apply complete"
}

generate_tfvars() {
    log_info "Generating terraform.tfvars (gitignored, will not be committed)..."

    ECR_REGISTRY="${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com"

    cat > "${TF_DIR}/terraform.tfvars" <<TFVARS
# Auto-generated by deploy-aws.sh — this file is gitignored
# Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

region      = "${REGION}"
name_prefix = "${NAME_PREFIX}"

# Container images (ECR)
tmi_image_url   = "${ECR_REGISTRY}/${NAME_PREFIX}-server:latest"
redis_image_url = "${ECR_REGISTRY}/${NAME_PREFIX}-redis:latest"

# Database
db_instance_class = "${DB_INSTANCE_CLASS}"
db_multi_az       = ${DB_MULTI_AZ}

# Network
enable_multi_az_nat = ${MULTI_AZ_NAT}
TFVARS

    # Certificate configuration (optional)
    if [[ -n "${DOMAIN}" ]]; then
        cat >> "${TF_DIR}/terraform.tfvars" <<TFVARS

# Certificate (ACM)
enable_certificate_automation = true
domain_name                   = "${DOMAIN}"
TFVARS
        if [[ -n "${ZONE_ID}" ]]; then
            echo "dns_zone_id                   = \"${ZONE_ID}\"" >> "${TF_DIR}/terraform.tfvars"
        fi
        if [[ -n "${SAN}" ]]; then
            # Convert comma-separated SANs to Terraform list
            local san_list
            san_list=$(echo "${SAN}" | sed 's/,/", "/g')
            echo "subject_alternative_names     = [\"${san_list}\"]" >> "${TF_DIR}/terraform.tfvars"
        fi
    fi

    # Alert email (optional)
    if [[ -n "${ALERT_EMAIL}" ]]; then
        echo "" >> "${TF_DIR}/terraform.tfvars"
        echo "# Alerting" >> "${TF_DIR}/terraform.tfvars"
        echo "alert_email = \"${ALERT_EMAIL}\"" >> "${TF_DIR}/terraform.tfvars"
    fi

    log_success "Generated terraform.tfvars"
}

cleanup_tfplan() {
    rm -f "${TF_DIR}/tfplan"
}

# ============================================================================
# PHASE 4: Verification
# ============================================================================

verify_deployment() {
    if [[ "${DESTROY}" == "true" ]] || [[ "${DRY_RUN}" == "true" ]]; then
        return 0
    fi

    log_step "Phase 4: Deployment Verification"

    cd "${TF_DIR}"

    # 1. Check Terraform outputs
    log_info "Checking Terraform outputs..."
    local lb_hostname
    lb_hostname=$(terraform output -raw load_balancer_hostname 2>/dev/null || echo "")
    if [[ -n "${lb_hostname}" ]] && [[ "${lb_hostname}" != "null" ]]; then
        log_success "Load balancer hostname: ${lb_hostname}"
    else
        log_warning "Load balancer hostname not yet available (may take a few minutes)"
    fi

    # 2. Configure kubectl
    log_info "Configuring kubectl..."
    local kubeconfig_cmd
    kubeconfig_cmd=$(terraform output -json useful_commands 2>/dev/null | jq -r '.kubeconfig_setup' 2>/dev/null || echo "")
    if [[ -n "${kubeconfig_cmd}" ]]; then
        eval "${kubeconfig_cmd}" 2>/dev/null || true
        log_success "kubectl configured for EKS cluster"

        # 3. Check pod status
        log_info "Checking pod status..."
        if kubectl get pods -n tmi 2>/dev/null; then
            echo ""

            # Wait briefly for pods to start
            local ready_pods
            ready_pods=$(kubectl get pods -n tmi --no-headers 2>/dev/null | grep -c "Running" || echo "0")
            local total_pods
            total_pods=$(kubectl get pods -n tmi --no-headers 2>/dev/null | wc -l | tr -d ' ')

            if [[ "${ready_pods}" -gt 0 ]]; then
                log_success "Pods running: ${ready_pods}/${total_pods}"
            else
                log_warning "No pods in Running state yet. EKS Fargate pods may take 2-5 minutes to start."
                echo "  Monitor with: kubectl get pods -n tmi -w"
            fi
        else
            log_warning "Cannot reach Kubernetes cluster yet"
            echo "  The cluster may still be provisioning. Try:"
            echo "  ${kubeconfig_cmd}"
            echo "  kubectl get pods -n tmi"
        fi
    else
        log_warning "Could not determine kubeconfig command"
    fi

    # 4. Test application endpoint
    if [[ -n "${lb_hostname}" ]] && [[ "${lb_hostname}" != "null" ]]; then
        log_info "Testing application endpoint..."
        local http_code
        http_code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 10 "http://${lb_hostname}/" 2>/dev/null || echo "000")
        if [[ "${http_code}" == "200" ]]; then
            log_success "Application responding (HTTP ${http_code})"
            echo ""
            echo -e "  ${GREEN}${BOLD}TMI is deployed and accessible at:${NC}"
            if [[ -n "${DOMAIN}" ]]; then
                echo -e "  ${BOLD}https://${DOMAIN}${NC}"
                echo ""
                echo "  Make sure your DNS CNAME record points to: ${lb_hostname}"
            else
                echo -e "  ${BOLD}http://${lb_hostname}${NC}"
            fi
        elif [[ "${http_code}" == "000" ]]; then
            log_warning "Application not yet responding (load balancer may still be provisioning)"
            echo "  This can take 3-5 minutes. Test with:"
            echo "  curl -v http://${lb_hostname}/"
        else
            log_warning "Application returned HTTP ${http_code}"
            echo "  Check pod logs: kubectl logs -n tmi -l app=tmi-api --tail=50"
        fi
    fi

    # 5. Summary
    echo ""
    log_step "Deployment Summary"
    echo "  Region:          ${REGION}"
    echo "  Name prefix:     ${NAME_PREFIX}"
    echo "  EKS cluster:     ${NAME_PREFIX}-eks"
    echo "  RDS instance:    ${NAME_PREFIX}-db (${DB_INSTANCE_CLASS})"
    if [[ -n "${DOMAIN}" ]]; then
        echo "  Domain:          ${DOMAIN}"
        echo "  Certificate:     ACM (auto-renewing)"
    fi
    if [[ -n "${lb_hostname}" ]] && [[ "${lb_hostname}" != "null" ]]; then
        echo "  Load balancer:   ${lb_hostname}"
    fi
    echo ""
    echo "  Useful commands:"
    echo "    kubectl get pods -n tmi              # Check pod status"
    echo "    kubectl logs -n tmi -l app=tmi-api   # View API logs"
    echo "    terraform output -json useful_commands # All commands"
    echo "    ./scripts/deploy-aws.sh --destroy     # Tear down"
    echo ""
}

# ============================================================================
# Main
# ============================================================================

main() {
    echo -e "${BOLD}TMI AWS Deployment${NC}"
    echo ""

    parse_args "$@"

    preflight_checks
    setup_ecr_and_push
    terraform_deploy
    verify_deployment

    if [[ "${DESTROY}" == "false" ]] && [[ "${DRY_RUN}" == "false" ]]; then
        log_success "Deployment complete!"
    fi
}

main "$@"
