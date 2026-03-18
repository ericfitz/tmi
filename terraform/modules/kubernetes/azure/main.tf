# Azure Kubernetes (AKS) Module for TMI
# Creates an AKS cluster with a single node pool and NGINX Ingress Controller

terraform {
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
  }
}

# AKS Cluster
resource "azurerm_kubernetes_cluster" "tmi" {
  name                = "${var.name_prefix}-aks"
  resource_group_name = var.resource_group_name
  location            = var.location
  dns_prefix          = var.name_prefix
  kubernetes_version  = var.kubernetes_version
  sku_tier            = var.aks_sku_tier

  default_node_pool {
    name                        = "default"
    node_count                  = var.node_count
    vm_size                     = var.node_vm_size
    vnet_subnet_id              = var.aks_subnet_id
    os_disk_size_gb             = var.os_disk_size_gb
    temporary_name_for_rotation = "temppool"

    upgrade_settings {
      max_surge = "1"
    }
  }

  identity {
    type = "SystemAssigned"
  }

  network_profile {
    network_plugin = "azure"
    service_cidr   = var.k8s_service_cidr
    dns_service_ip = var.k8s_dns_service_ip
  }

  # Private cluster configuration
  private_cluster_enabled             = var.private_cluster_enabled
  private_cluster_public_fqdn_enabled = var.private_cluster_enabled

  # API server authorized IP ranges (for private clusters during provisioning)
  dynamic "api_server_access_profile" {
    for_each = var.api_server_authorized_ip_ranges != null ? [1] : []
    content {
      authorized_ip_ranges = var.api_server_authorized_ip_ranges
    }
  }

  tags = var.tags

  lifecycle {
    ignore_changes = [
      # Ignore changes to authorized IP ranges (managed by null_resource for private clusters)
      api_server_access_profile,
    ]
  }
}

# ACR Pull role assignment (allows AKS to pull images from ACR)
resource "azurerm_role_assignment" "acr_pull" {
  count                            = var.acr_id != null ? 1 : 0
  principal_id                     = azurerm_kubernetes_cluster.tmi.kubelet_identity[0].object_id
  role_definition_name             = "AcrPull"
  scope                            = var.acr_id
  skip_service_principal_aad_check = true
}

# NGINX Ingress Controller via Helm
resource "helm_release" "nginx_ingress" {
  name             = "ingress-nginx"
  repository       = "https://kubernetes.github.io/ingress-nginx"
  chart            = "ingress-nginx"
  namespace        = "ingress-nginx"
  create_namespace = true
  version          = var.nginx_ingress_chart_version

  set {
    name  = "controller.service.type"
    value = var.private_cluster_enabled ? "ClusterIP" : "LoadBalancer"
  }

  set {
    name  = "controller.service.annotations.service\\.beta\\.kubernetes\\.io/azure-load-balancer-health-probe-request-path"
    value = "/healthz"
  }

  # Internal LB for private clusters
  dynamic "set" {
    for_each = var.private_cluster_enabled ? [1] : []
    content {
      name  = "controller.service.annotations.service\\.beta\\.kubernetes\\.io/azure-load-balancer-internal"
      value = "true"
    }
  }

  depends_on = [azurerm_kubernetes_cluster.tmi]
}
