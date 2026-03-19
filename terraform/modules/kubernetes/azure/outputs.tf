# Outputs for Azure Kubernetes (AKS) Module

# AKS Cluster
output "cluster_id" {
  description = "ID of the AKS cluster"
  value       = azurerm_kubernetes_cluster.tmi.id
}

output "cluster_name" {
  description = "Name of the AKS cluster"
  value       = azurerm_kubernetes_cluster.tmi.name
}

output "cluster_endpoint" {
  description = "Kubernetes API endpoint of the AKS cluster"
  value       = azurerm_kubernetes_cluster.tmi.kube_config[0].host
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate for the AKS cluster"
  value       = azurerm_kubernetes_cluster.tmi.kube_config[0].cluster_ca_certificate
  sensitive   = true
}

output "kube_config_raw" {
  description = "Raw kubeconfig content for kubectl access"
  value       = azurerm_kubernetes_cluster.tmi.kube_config_raw
  sensitive   = true
}

output "kubelet_identity_object_id" {
  description = "Object ID of the kubelet managed identity (for ACR pull, Key Vault access)"
  value       = azurerm_kubernetes_cluster.tmi.kubelet_identity[0].object_id
}

output "identity_principal_id" {
  description = "Principal ID of the AKS cluster system-assigned managed identity"
  value       = azurerm_kubernetes_cluster.tmi.identity[0].principal_id
}

# Namespace
output "namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = kubernetes_namespace_v1.tmi.metadata[0].name
}

# Standard interface outputs for compatibility
output "kubernetes_cluster_name" {
  description = "Cluster name for kubectl config (standard interface)"
  value       = azurerm_kubernetes_cluster.tmi.name
}

output "kubernetes_config_command" {
  description = "Command to configure kubectl (standard interface)"
  value       = "az aks get-credentials --resource-group ${var.resource_group_name} --name ${azurerm_kubernetes_cluster.tmi.name}"
}
