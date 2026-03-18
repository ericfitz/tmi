# Outputs for TMI Azure Public Deployment

# Standard outputs (per spec Section 7)
output "tmi_api_endpoint" {
  description = "URL to reach the TMI API"
  value       = "http://${module.kubernetes.cluster_name}.${var.location}.cloudapp.azure.com"
}

output "tmi_external_url" {
  description = "Internet-accessible URL for TMI"
  value       = "http://${module.kubernetes.cluster_name}.${var.location}.cloudapp.azure.com"
}

output "kubernetes_cluster_name" {
  description = "AKS cluster name"
  value       = module.kubernetes.kubernetes_cluster_name
}

output "kubernetes_config_command" {
  description = "Command to configure kubectl"
  value       = module.kubernetes.kubernetes_config_command
}

output "database_host" {
  description = "PostgreSQL connection endpoint (internal)"
  value       = module.database.database_host
}

output "container_registry_url" {
  description = "ACR login server URL for pushing images"
  value       = azurerm_container_registry.tmi.login_server
}

output "redis_endpoint" {
  description = "Internal Redis service address"
  value       = "tmi-redis.tmi.svc.cluster.local:6379"
}

# Resource Group
output "resource_group_name" {
  description = "Name of the resource group"
  value       = azurerm_resource_group.tmi.name
}

# Logging
output "log_analytics_workspace_id" {
  description = "ID of the Log Analytics Workspace"
  value       = module.logging.workspace_id
}

# Secrets
output "key_vault_name" {
  description = "Name of the Key Vault"
  value       = module.secrets.key_vault_name
}

# Useful commands
output "useful_commands" {
  description = "Useful commands for managing the deployment"
  value = {
    kubeconfig_setup = module.kubernetes.kubernetes_config_command
    kubectl_pods     = "kubectl get pods -n tmi"
    kubectl_logs_api = "kubectl logs -n tmi -l app=tmi-api --tail=100"
    acr_login        = "az acr login --name ${azurerm_container_registry.tmi.name}"
    acr_push         = "docker push ${azurerm_container_registry.tmi.login_server}/tmi:latest"
  }
}
