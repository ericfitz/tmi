# Azure Network Module for TMI
# Creates VNet with subnets, NAT Gateway, and Network Security Groups

terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 3.80.0"
    }
  }
}

# Virtual Network
resource "azurerm_virtual_network" "tmi" {
  name                = "${var.name_prefix}-vnet"
  resource_group_name = var.resource_group_name
  location            = var.location
  address_space       = [var.vnet_cidr]

  tags = var.tags
}

# AKS Subnet
resource "azurerm_subnet" "aks" {
  name                 = "${var.name_prefix}-aks"
  resource_group_name  = var.resource_group_name
  virtual_network_name = azurerm_virtual_network.tmi.name
  address_prefixes     = [var.aks_subnet_cidr]

  service_endpoints = var.enable_private_endpoints ? ["Microsoft.Sql", "Microsoft.KeyVault"] : []
}

# Database Subnet
resource "azurerm_subnet" "database" {
  name                 = "${var.name_prefix}-database"
  resource_group_name  = var.resource_group_name
  virtual_network_name = azurerm_virtual_network.tmi.name
  address_prefixes     = [var.database_subnet_cidr]

  service_endpoints = ["Microsoft.Storage"]

  delegation {
    name = "postgresql-delegation"
    service_delegation {
      name = "Microsoft.DBforPostgreSQL/flexibleServers"
      actions = [
        "Microsoft.Network/virtualNetworks/subnets/join/action",
      ]
    }
  }
}

# Public IP for NAT Gateway
resource "azurerm_public_ip" "nat" {
  name                = "${var.name_prefix}-nat-ip"
  resource_group_name = var.resource_group_name
  location            = var.location
  allocation_method   = "Static"
  sku                 = "Standard"

  tags = var.tags
}

# NAT Gateway
resource "azurerm_nat_gateway" "tmi" {
  name                = "${var.name_prefix}-nat"
  resource_group_name = var.resource_group_name
  location            = var.location
  sku_name            = "Standard"

  tags = var.tags
}

# Associate NAT Gateway with public IP
resource "azurerm_nat_gateway_public_ip_association" "tmi" {
  nat_gateway_id       = azurerm_nat_gateway.tmi.id
  public_ip_address_id = azurerm_public_ip.nat.id
}

# Associate NAT Gateway with AKS subnet
resource "azurerm_subnet_nat_gateway_association" "aks" {
  subnet_id      = azurerm_subnet.aks.id
  nat_gateway_id = azurerm_nat_gateway.tmi.id
}

# Network Security Group for AKS
resource "azurerm_network_security_group" "aks" {
  name                = "${var.name_prefix}-aks-nsg"
  resource_group_name = var.resource_group_name
  location            = var.location

  # Allow HTTP from internet (public) or deployer CIDR (private)
  security_rule {
    name                       = "allow-http-inbound"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "80"
    source_address_prefix      = var.enable_private_endpoints ? var.allowed_cidr : "*"
    destination_address_prefix = "*"
  }

  # Allow HTTPS from internet (public) or deployer CIDR (private)
  security_rule {
    name                       = "allow-https-inbound"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "443"
    source_address_prefix      = var.enable_private_endpoints ? var.allowed_cidr : "*"
    destination_address_prefix = "*"
  }

  # Allow TMI server port from within VNet
  security_rule {
    name                       = "allow-tmi-inbound"
    priority                   = 120
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "8080"
    source_address_prefix      = var.vnet_cidr
    destination_address_prefix = "*"
  }

  # Allow outbound to PostgreSQL
  security_rule {
    name                       = "allow-postgresql-outbound"
    priority                   = 100
    direction                  = "Outbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "5432"
    source_address_prefix      = var.aks_subnet_cidr
    destination_address_prefix = var.database_subnet_cidr
  }

  # Allow outbound HTTPS (OAuth IdPs, container registries, Azure services)
  security_rule {
    name                       = "allow-https-outbound"
    priority                   = 110
    direction                  = "Outbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "443"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  tags = var.tags
}

# Associate NSG with AKS subnet
resource "azurerm_subnet_network_security_group_association" "aks" {
  subnet_id                 = azurerm_subnet.aks.id
  network_security_group_id = azurerm_network_security_group.aks.id
}

# Network Security Group for Database
resource "azurerm_network_security_group" "database" {
  name                = "${var.name_prefix}-database-nsg"
  resource_group_name = var.resource_group_name
  location            = var.location

  # Allow PostgreSQL from AKS subnet only
  security_rule {
    name                       = "allow-postgresql-inbound"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "5432"
    source_address_prefix      = var.aks_subnet_cidr
    destination_address_prefix = "*"
  }

  # Deny all other inbound
  security_rule {
    name                       = "deny-all-inbound"
    priority                   = 4096
    direction                  = "Inbound"
    access                     = "Deny"
    protocol                   = "*"
    source_port_range          = "*"
    destination_port_range     = "*"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  tags = var.tags
}

# Associate NSG with Database subnet
resource "azurerm_subnet_network_security_group_association" "database" {
  subnet_id                 = azurerm_subnet.database.id
  network_security_group_id = azurerm_network_security_group.database.id
}

# Private DNS Zone for PostgreSQL (required for VNet integration)
resource "azurerm_private_dns_zone" "postgresql" {
  name                = "${var.name_prefix}.private.postgres.database.azure.com"
  resource_group_name = var.resource_group_name

  tags = var.tags
}

# Link Private DNS Zone to VNet
resource "azurerm_private_dns_zone_virtual_network_link" "postgresql" {
  name                  = "${var.name_prefix}-pg-dns-link"
  resource_group_name   = var.resource_group_name
  private_dns_zone_name = azurerm_private_dns_zone.postgresql.name
  virtual_network_id    = azurerm_virtual_network.tmi.id
  registration_enabled  = false

  tags = var.tags
}
