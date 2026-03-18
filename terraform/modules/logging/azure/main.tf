# Azure Logging Module for TMI
# Creates Log Analytics Workspace and Container Insights configuration

terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 3.80.0"
    }
  }
}

# Log Analytics Workspace
resource "azurerm_log_analytics_workspace" "tmi" {
  name                = "${var.name_prefix}-logs"
  resource_group_name = var.resource_group_name
  location            = var.location
  sku                 = "PerGB2018"
  retention_in_days   = var.retention_days

  tags = var.tags
}

# Log Analytics Solution for Container Insights
resource "azurerm_log_analytics_solution" "containers" {
  count                 = var.enable_container_insights ? 1 : 0
  solution_name         = "ContainerInsights"
  workspace_resource_id = azurerm_log_analytics_workspace.tmi.id
  workspace_name        = azurerm_log_analytics_workspace.tmi.name
  resource_group_name   = var.resource_group_name
  location              = var.location

  plan {
    publisher = "Microsoft"
    product   = "OMSGallery/ContainerInsights"
  }

  tags = var.tags
}
