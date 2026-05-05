# TMI GCP Public Deployment
# Low-cost "kick the tires" deployment on GKE Autopilot with Cloud SQL PostgreSQL
# No deletion protection — easy teardown with `terraform destroy`

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
  }

  # T6/T11 (#344): remote, encrypted state is the default. See gcp-private
  # for backend-config conventions.
  backend "gcs" {
    prefix = "tmi/gcp-public"
  }
}

# GCP Provider
provider "google" {
  project = var.project_id
  region  = var.region
}

# Kubernetes Provider - configured after GKE cluster creation
provider "kubernetes" {
  host                   = module.kubernetes.cluster_endpoint
  cluster_ca_certificate = base64decode(module.kubernetes.cluster_ca_certificate)
  token                  = module.kubernetes.access_token
}

locals {
  labels = merge(var.labels, {
    project     = "tmi"
    environment = "public"
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
  description   = "Container images for TMI deployment"
  format        = "DOCKER"

  labels = local.labels
}

# ============================================================================
# Network Module
# ============================================================================

module "network" {
  source = "../../modules/network/gcp"

  project_id            = var.project_id
  region                = var.region
  name_prefix           = var.name_prefix
  primary_subnet_cidr   = var.primary_subnet_cidr
  pods_cidr             = var.pods_cidr
  services_cidr         = var.services_cidr
  enable_public_ingress = true

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
# Database Module
# ============================================================================

module "database" {
  source = "../../modules/database/gcp"

  project_id          = var.project_id
  region              = var.region
  name_prefix         = var.name_prefix
  tier                = "db-custom-1-3840"
  deletion_protection = false
  enable_public_ip    = true
  database_name       = var.database_name
  db_username         = var.db_username
  db_password         = module.secrets.db_password

  labels = local.labels
}

# ============================================================================
# Kubernetes (GKE Autopilot) Module
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

  # Cluster configuration
  deletion_protection = false

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

  # Build mode: dev for public template (verbose logging, relaxed security)
  tmi_build_mode              = "dev"
  extra_environment_variables = var.extra_env_vars

  # Public load balancer
  enable_internal_lb = false

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
