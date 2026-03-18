# TMI Azure Private Deployment
# Private cluster deployment with no public ingress
# AKS Standard tier, private cluster, private endpoints, deletion protection

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 3.80.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = ">= 2.12.0"
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
  # backend "azurerm" {
  #   resource_group_name  = "tmi-terraform-state"
  #   storage_account_name = "tmitfstate"
  #   container_name       = "tfstate"
  #   key                  = "azure-private/terraform.tfstate"
  # }
}

# Azure Provider
provider "azurerm" {
  features {
    key_vault {
      purge_soft_delete_on_destroy = false
    }
  }
  subscription_id = var.subscription_id
}

# Kubernetes Provider - configured after AKS creation
provider "kubernetes" {
  host                   = module.kubernetes.cluster_endpoint
  cluster_ca_certificate = base64decode(module.kubernetes.cluster_ca_certificate)
  client_certificate     = base64decode(data.azurerm_kubernetes_cluster.tmi.kube_config[0].client_certificate)
  client_key             = base64decode(data.azurerm_kubernetes_cluster.tmi.kube_config[0].client_key)
}

# Helm Provider
provider "helm" {
  kubernetes {
    host                   = module.kubernetes.cluster_endpoint
    cluster_ca_certificate = base64decode(module.kubernetes.cluster_ca_certificate)
    client_certificate     = base64decode(data.azurerm_kubernetes_cluster.tmi.kube_config[0].client_certificate)
    client_key             = base64decode(data.azurerm_kubernetes_cluster.tmi.kube_config[0].client_key)
  }
}

# Data source to get AKS kube_config after cluster creation
data "azurerm_kubernetes_cluster" "tmi" {
  name                = module.kubernetes.cluster_name
  resource_group_name = azurerm_resource_group.tmi.name

  depends_on = [module.kubernetes]
}

# Detect deployer's public IP for temporary API server access
data "http" "deployer_ip" {
  url = "https://checkip.amazonaws.com"
}

locals {
  deployer_ip = "${chomp(data.http.deployer_ip.response_body)}/32"

  tags = merge(var.tags, {
    project     = "tmi"
    environment = "private"
    managed_by  = "terraform"
  })
}

# Resource Group
resource "azurerm_resource_group" "tmi" {
  name     = "${var.name_prefix}-rg"
  location = var.location

  tags = local.tags
}

# Azure Container Registry (Basic SKU with private endpoint)
resource "azurerm_container_registry" "tmi" {
  name                          = "${replace(var.name_prefix, "-", "")}acr"
  resource_group_name           = azurerm_resource_group.tmi.name
  location                      = azurerm_resource_group.tmi.location
  sku                           = "Basic"
  admin_enabled                 = false
  public_network_access_enabled = true # Basic SKU does not support private endpoints

  tags = local.tags
}

# Network Module
module "network" {
  source = "../../modules/network/azure"

  resource_group_name      = azurerm_resource_group.tmi.name
  location                 = azurerm_resource_group.tmi.location
  name_prefix              = var.name_prefix
  vnet_cidr                = var.vnet_cidr
  aks_subnet_cidr          = var.aks_subnet_cidr
  database_subnet_cidr     = var.database_subnet_cidr
  enable_private_endpoints = true
  allowed_cidr             = var.allowed_cidr

  tags = local.tags
}

# Secrets Module
module "secrets" {
  source = "../../modules/secrets/azure"

  resource_group_name = azurerm_resource_group.tmi.name
  location            = azurerm_resource_group.tmi.location
  name_prefix         = var.name_prefix

  db_password    = var.db_password
  redis_password = var.redis_password
  jwt_secret     = var.jwt_secret

  purge_protection_enabled = true

  tags = local.tags
}

# Database Module
module "database" {
  source = "../../modules/database/azure"

  resource_group_name   = azurerm_resource_group.tmi.name
  location              = azurerm_resource_group.tmi.location
  name_prefix           = var.name_prefix
  admin_username        = var.db_username
  admin_password        = module.secrets.db_password
  database_name         = var.db_name
  sku_name              = "B_Standard_B2s"
  enable_private_access = true
  database_subnet_id    = module.network.database_subnet_id
  private_dns_zone_id   = module.network.postgresql_private_dns_zone_id
  deletion_protection   = true

  tags = local.tags

  depends_on = [module.network]
}

# Logging Module
module "logging" {
  source = "../../modules/logging/azure"

  resource_group_name       = azurerm_resource_group.tmi.name
  location                  = azurerm_resource_group.tmi.location
  name_prefix               = var.name_prefix
  retention_days            = 90
  enable_container_insights = true

  tags = local.tags
}

# Kubernetes Module
module "kubernetes" {
  source = "../../modules/kubernetes/azure"

  resource_group_name = azurerm_resource_group.tmi.name
  location            = azurerm_resource_group.tmi.location
  name_prefix         = var.name_prefix

  # AKS cluster configuration - Standard tier with private cluster
  kubernetes_version      = var.kubernetes_version
  aks_sku_tier            = "Standard"
  node_count              = 1
  node_vm_size            = "Standard_B2ms"
  aks_subnet_id           = module.network.aks_subnet_id
  private_cluster_enabled = true

  # Temporary API server access for provisioning
  api_server_authorized_ip_ranges = [local.deployer_ip]

  # ACR integration
  acr_id = azurerm_container_registry.tmi.id

  # Container images
  tmi_image_url   = var.tmi_image_url
  redis_image_url = var.redis_image_url

  # Redis configuration
  redis_password = module.secrets.redis_password

  # Database configuration
  db_username = var.db_username
  db_password = module.secrets.db_password
  db_host     = module.database.server_fqdn
  db_name     = var.db_name

  # Secrets
  jwt_secret = module.secrets.jwt_secret

  # Build mode - production for private template
  tmi_build_mode              = "production"
  extra_environment_variables = var.extra_env_vars

  tags = local.tags

  depends_on = [module.network, module.database, module.secrets]
}

# Management lock on resource group for additional protection
resource "azurerm_management_lock" "rg" {
  name       = "${var.name_prefix}-rg-lock"
  scope      = azurerm_resource_group.tmi.id
  lock_level = "CanNotDelete"
  notes      = "Deletion protection for TMI private deployment"
}

# Remove temporary API server access after all K8s resources are provisioned
resource "null_resource" "remove_api_access" {
  triggers = {
    cluster_name        = module.kubernetes.cluster_name
    resource_group_name = azurerm_resource_group.tmi.name
  }

  provisioner "local-exec" {
    command = "az aks update --resource-group ${azurerm_resource_group.tmi.name} --name ${module.kubernetes.cluster_name} --api-server-authorized-ip-ranges ''"
  }

  depends_on = [module.kubernetes]
}
