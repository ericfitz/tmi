# TMI GCP Private Deployment
# Private GKE Autopilot cluster with Cloud SQL private IP, deletion protection ON
# No public internet-facing load balancer or public IP on the cluster

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0.0"
    }
    null = {
      source  = "hashicorp/null"
      version = ">= 3.0.0"
    }
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0.0"
    }
  }

  # Uncomment and configure for remote state
  # backend "gcs" {
  #   bucket = "your-terraform-state-bucket"
  #   prefix = "tmi/gcp-private"
  # }
}

# GCP Provider
provider "google" {
  project = var.project_id
  region  = var.region
}

# Kubernetes Provider - configured after GKE cluster creation
# Uses temporary authorized network entry for deployer IP during provisioning
provider "kubernetes" {
  host                   = module.kubernetes.cluster_endpoint
  cluster_ca_certificate = base64decode(module.kubernetes.cluster_ca_certificate)
  token                  = module.kubernetes.access_token
}

# Detect deployer's public IP for temporary K8s API access
data "http" "deployer_ip" {
  url = "https://checkip.amazonaws.com"
}

locals {
  deployer_ip = "${chomp(data.http.deployer_ip.response_body)}/32"

  labels = merge(var.labels, {
    project     = "tmi"
    environment = "private"
    managed_by  = "terraform"
  })
}

# ============================================================================
# Artifact Registry (container registry - created in root template per spec)
# ============================================================================

resource "google_artifact_registry_repository" "tmi" {
  project       = var.project_id
  location      = var.region
  repository_id = "${var.name_prefix}-containers"
  description   = "Container images for TMI private deployment"
  format        = "DOCKER"

  labels = local.labels
}

# ============================================================================
# Network Module (private cluster networking)
# ============================================================================

module "network" {
  source = "../../modules/network/gcp"

  project_id            = var.project_id
  region                = var.region
  name_prefix           = var.name_prefix
  primary_subnet_cidr   = var.primary_subnet_cidr
  pods_cidr             = var.pods_cidr
  services_cidr         = var.services_cidr
  enable_public_ingress = false
  private_ingress_cidrs = var.private_ingress_cidrs

  # Enable Private Service Access for Cloud SQL private IP
  enable_private_services_access = true

  labels = local.labels
}

# ============================================================================
# Secrets Module
# ============================================================================

module "secrets" {
  source = "../../modules/secrets/gcp"

  project_id     = var.project_id
  name_prefix    = var.name_prefix
  db_password    = var.db_password
  redis_password = var.redis_password
  jwt_secret     = var.jwt_secret

  labels = local.labels
}

# ============================================================================
# Database Module (private IP only, deletion protection ON)
# ============================================================================

module "database" {
  source = "../../modules/database/gcp"

  project_id          = var.project_id
  region              = var.region
  name_prefix         = var.name_prefix
  tier                = "db-custom-1-3840"
  deletion_protection = true
  enable_public_ip    = false
  private_network_id  = module.network.network_self_link
  database_name       = var.database_name
  db_username         = var.db_username
  db_password         = module.secrets.db_password

  # Temporary authorized network for deployer IP (needed for initial setup)
  authorized_networks = [
    {
      name = "deployer"
      cidr = local.deployer_ip
    }
  ]

  labels = local.labels

  depends_on = [module.network]
}

# ============================================================================
# Kubernetes (GKE Autopilot) Module - Private Cluster
# ============================================================================

module "kubernetes" {
  source = "../../modules/kubernetes/gcp"

  project_id  = var.project_id
  region      = var.region
  name_prefix = var.name_prefix

  # Network configuration
  network_id          = module.network.network_id
  subnetwork_id       = module.network.primary_subnet_id
  pods_range_name     = module.network.pods_range_name
  services_range_name = module.network.services_range_name

  # Private cluster configuration
  enable_private_cluster  = true
  enable_private_endpoint = false # Keep public endpoint for provisioning, removed after
  master_ipv4_cidr_block  = var.master_ipv4_cidr_block

  # Temporary authorized network for deployer
  master_authorized_cidrs = [
    {
      name = "deployer"
      cidr = local.deployer_ip
    }
  ]

  # Cluster configuration
  deletion_protection = true

  # Container images
  tmi_image_url   = var.tmi_image_url
  redis_image_url = var.redis_image_url
  tmi_replicas    = 1

  # Database configuration
  db_username = var.db_username
  db_password = module.secrets.db_password
  db_host     = module.database.database_host
  db_name     = var.database_name

  # Redis configuration
  redis_password = module.secrets.redis_password

  # Secrets configuration
  jwt_secret = module.secrets.jwt_secret

  # Build mode: production for private template
  tmi_build_mode              = "production"
  extra_environment_variables = var.extra_env_vars

  # Internal load balancer (no public ingress)
  enable_internal_lb = true

  labels = local.labels

  depends_on = [module.network, module.database, module.secrets]
}

# ============================================================================
# Secrets IAM - Grant Workload Identity access after cluster creation
# ============================================================================

resource "google_secret_manager_secret_iam_member" "db_password_accessor" {
  project   = var.project_id
  secret_id = module.secrets.secret_names.db_password
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${module.kubernetes.workload_identity_sa_email}"

  depends_on = [module.kubernetes, module.secrets]
}

resource "google_secret_manager_secret_iam_member" "redis_password_accessor" {
  project   = var.project_id
  secret_id = module.secrets.secret_names.redis_password
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${module.kubernetes.workload_identity_sa_email}"

  depends_on = [module.kubernetes, module.secrets]
}

resource "google_secret_manager_secret_iam_member" "jwt_secret_accessor" {
  project   = var.project_id
  secret_id = module.secrets.secret_names.jwt_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${module.kubernetes.workload_identity_sa_email}"

  depends_on = [module.kubernetes, module.secrets]
}

# ============================================================================
# Logging Module
# ============================================================================

module "logging" {
  source = "../../modules/logging/gcp"

  project_id     = var.project_id
  region         = var.region
  name_prefix    = var.name_prefix
  create_metrics = true
  create_alerts  = false

  labels = local.labels
}

# ============================================================================
# Temporary K8s API Access Removal
# After all K8s resources are provisioned, remove the deployer's authorized
# network entry from the GKE master_authorized_networks_config.
# ============================================================================

resource "null_resource" "remove_deployer_access" {
  triggers = {
    cluster_name = module.kubernetes.cluster_name
    project_id   = var.project_id
    region       = var.region
  }

  provisioner "local-exec" {
    command = <<-EOT
      echo "Removing temporary deployer access from GKE master authorized networks..."
      gcloud container clusters update ${module.kubernetes.cluster_name} \
        --region ${var.region} \
        --project ${var.project_id} \
        --no-enable-master-authorized-networks \
        --quiet || echo "Warning: Failed to remove authorized networks. Manual cleanup may be needed."
    EOT
  }

  depends_on = [
    module.kubernetes,
    google_secret_manager_secret_iam_member.db_password_accessor,
    google_secret_manager_secret_iam_member.redis_password_accessor,
    google_secret_manager_secret_iam_member.jwt_secret_accessor,
  ]
}
