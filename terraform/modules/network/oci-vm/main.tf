# OCI Network Module for TMI
# Creates VCN with public, private, and database subnets
# Includes gateways, route tables, and network security groups

terraform {
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0.0"
    }
  }
}

# Data source for availability domains
data "oci_identity_availability_domains" "ads" {
  compartment_id = var.compartment_id
}

# Data source for OCI services (for Service Gateway)
data "oci_core_services" "all_services" {
  filter {
    name   = "name"
    values = ["All .* Services In Oracle Services Network"]
    regex  = true
  }
}

# VCN
resource "oci_core_vcn" "tmi" {
  compartment_id = var.compartment_id
  cidr_blocks    = [var.vcn_cidr]
  display_name   = "${var.name_prefix}-vcn"
  dns_label      = var.dns_label

  freeform_tags = var.tags
}

# Internet Gateway
resource "oci_core_internet_gateway" "tmi" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-igw"
  enabled        = true

  freeform_tags = var.tags
}

# Service Gateway (for OCI services like Object Storage)
resource "oci_core_service_gateway" "tmi" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-sgw"

  services {
    service_id = data.oci_core_services.all_services.services[0].id
  }

  freeform_tags = var.tags
}

# Route Table for Public Subnet
resource "oci_core_route_table" "public" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-public-rt"

  route_rules {
    network_entity_id = oci_core_internet_gateway.tmi.id
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
  }

  freeform_tags = var.tags
}

# Route Table for Private Subnet
resource "oci_core_route_table" "private" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-private-rt"

  # Service Gateway for OCI services (NAT Gateway removed — no resources in private subnet)
  route_rules {
    network_entity_id = oci_core_service_gateway.tmi.id
    destination       = data.oci_core_services.all_services.services[0].cidr_block
    destination_type  = "SERVICE_CIDR_BLOCK"
  }

  freeform_tags = var.tags
}

# Security List for Public Subnet (VM lives here — NSGs handle fine-grained access control)
resource "oci_core_security_list" "public" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-public-sl"

  # Allow SSH
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = "0.0.0.0/0"
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 22
      max = 22
    }
  }

  # Allow HTTP/UX (port 80 and 4200)
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = "0.0.0.0/0"
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 80
      max = 80
    }
  }

  ingress_security_rules {
    protocol    = "6" # TCP
    source      = "0.0.0.0/0"
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 4200
      max = 4200
    }
  }

  # Allow TMI API
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = "0.0.0.0/0"
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 8080
      max = 8080
    }
  }

  # Allow HTTPS (for future TLS)
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = "0.0.0.0/0"
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 443
      max = 443
    }
  }

  # Allow all outbound
  egress_security_rules {
    protocol         = "all"
    destination      = "0.0.0.0/0"
    destination_type = "CIDR_BLOCK"
  }

  freeform_tags = var.tags
}

# Security List for Private Subnet
resource "oci_core_security_list" "private" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-private-sl"

  # Allow all traffic within VCN
  ingress_security_rules {
    protocol    = "all"
    source      = var.vcn_cidr
    source_type = "CIDR_BLOCK"
  }

  # Allow all outbound
  egress_security_rules {
    protocol         = "all"
    destination      = "0.0.0.0/0"
    destination_type = "CIDR_BLOCK"
  }

  freeform_tags = var.tags
}

# Security List for Database Subnet (kept for VCN completeness, no active use)
resource "oci_core_security_list" "database" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-database-sl"

  # Allow all outbound (for OCI services)
  egress_security_rules {
    protocol         = "all"
    destination      = "0.0.0.0/0"
    destination_type = "CIDR_BLOCK"
  }

  freeform_tags = var.tags
}

# Public Subnet (VM lives here with direct public IP)
resource "oci_core_subnet" "public" {
  compartment_id    = var.compartment_id
  vcn_id            = oci_core_vcn.tmi.id
  cidr_block        = var.public_subnet_cidr
  display_name      = "${var.name_prefix}-public"
  dns_label         = "public"
  route_table_id    = oci_core_route_table.public.id
  security_list_ids = [oci_core_security_list.public.id]

  freeform_tags = var.tags
}

# Private Subnet (kept for VCN completeness)
resource "oci_core_subnet" "private" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.tmi.id
  cidr_block                 = var.private_subnet_cidr
  display_name               = "${var.name_prefix}-private"
  dns_label                  = "private"
  route_table_id             = oci_core_route_table.private.id
  security_list_ids          = [oci_core_security_list.private.id]
  prohibit_public_ip_on_vnic = true

  freeform_tags = var.tags
}

# Database Subnet (kept for VCN completeness)
resource "oci_core_subnet" "database" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.tmi.id
  cidr_block                 = var.database_subnet_cidr
  display_name               = "${var.name_prefix}-database"
  dns_label                  = "database"
  route_table_id             = oci_core_route_table.private.id
  security_list_ids          = [oci_core_security_list.database.id]
  prohibit_public_ip_on_vnic = true

  freeform_tags = var.tags
}

# Network Security Group for TMI Server
resource "oci_core_network_security_group" "tmi_server" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-tmi-nsg"

  freeform_tags = var.tags
}

# NSG Rules for TMI Server — direct internet access (no Load Balancer)
resource "oci_core_network_security_group_security_rule" "tmi_ingress_ssh" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow SSH from internet (replaces OCI Bastion)"
  source      = "0.0.0.0/0"
  source_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 22
      max = 22
    }
  }
}

resource "oci_core_network_security_group_security_rule" "tmi_ingress_api" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow TMI API access from internet"
  source      = "0.0.0.0/0"
  source_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 8080
      max = 8080
    }
  }
}

resource "oci_core_network_security_group_security_rule" "tmi_ingress_ux" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow TMI-UX frontend access from internet"
  source      = "0.0.0.0/0"
  source_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 4200
      max = 4200
    }
  }
}

resource "oci_core_network_security_group_security_rule" "tmi_egress_redis" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow traffic to Redis (co-located on same host)"
  destination      = oci_core_network_security_group.redis.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 6379
      max = 6379
    }
  }
}

resource "oci_core_network_security_group_security_rule" "tmi_egress_https_internet" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow HTTPS to internet (OCIR pulls, OAuth, external APIs)"
  destination      = "0.0.0.0/0"
  destination_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 443
      max = 443
    }
  }
}

# Network Security Group for Redis (co-located on same VM)
resource "oci_core_network_security_group" "redis" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-redis-nsg"

  freeform_tags = var.tags
}

# NSG Rules for Redis
resource "oci_core_network_security_group_security_rule" "redis_ingress_tmi" {
  network_security_group_id = oci_core_network_security_group.redis.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow traffic from TMI server"
  source      = oci_core_network_security_group.tmi_server.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 6379
      max = 6379
    }
  }
}
