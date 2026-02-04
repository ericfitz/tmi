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

# NAT Gateway
resource "oci_core_nat_gateway" "tmi" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-nat"

  freeform_tags = var.tags
}

# Service Gateway (for OCI services like Vault, Logging, Object Storage)
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

  # NAT Gateway for outbound internet access
  route_rules {
    network_entity_id = oci_core_nat_gateway.tmi.id
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
  }

  # Service Gateway for OCI services
  route_rules {
    network_entity_id = oci_core_service_gateway.tmi.id
    destination       = data.oci_core_services.all_services.services[0].cidr_block
    destination_type  = "SERVICE_CIDR_BLOCK"
  }

  freeform_tags = var.tags
}

# Security List for Public Subnet (minimal - NSGs handle most rules)
resource "oci_core_security_list" "public" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-public-sl"

  # Allow HTTPS from anywhere (load balancer)
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = "0.0.0.0/0"
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 443
      max = 443
    }
  }

  # Allow HTTP for redirect (optional)
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = "0.0.0.0/0"
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 80
      max = 80
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

# Security List for Database Subnet
resource "oci_core_security_list" "database" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-database-sl"

  # Allow database traffic from private subnet only
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.private_subnet_cidr
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 1521
      max = 1522
    }
  }

  # Allow all outbound (for OCI services)
  egress_security_rules {
    protocol         = "all"
    destination      = "0.0.0.0/0"
    destination_type = "CIDR_BLOCK"
  }

  freeform_tags = var.tags
}

# Public Subnet (Load Balancer)
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

# Private Subnet (TMI Server, Redis)
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

# Database Subnet (ADB)
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

# NSG Rules for TMI Server
resource "oci_core_network_security_group_security_rule" "tmi_ingress_lb" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow traffic from load balancer"
  source      = oci_core_network_security_group.lb.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 8080
      max = 8080
    }
  }
}

resource "oci_core_network_security_group_security_rule" "tmi_egress_redis" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow traffic to Redis"
  destination      = oci_core_network_security_group.redis.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 6379
      max = 6379
    }
  }
}

resource "oci_core_network_security_group_security_rule" "tmi_egress_db" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow traffic to database"
  destination      = var.database_subnet_cidr
  destination_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 1521
      max = 1522
    }
  }
}

resource "oci_core_network_security_group_security_rule" "tmi_egress_services" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow HTTPS to OCI services"
  destination      = data.oci_core_services.all_services.services[0].cidr_block
  destination_type = "SERVICE_CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 443
      max = 443
    }
  }
}

# Allow HTTPS to internet (for Free Tier ADB public endpoint)
resource "oci_core_network_security_group_security_rule" "tmi_egress_https_internet" {
  network_security_group_id = oci_core_network_security_group.tmi_server.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow HTTPS to internet for ADB public endpoint"
  destination      = "0.0.0.0/0"
  destination_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 443
      max = 443
    }
  }
}

# Network Security Group for Redis
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

# Network Security Group for Load Balancer
resource "oci_core_network_security_group" "lb" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-lb-nsg"

  freeform_tags = var.tags
}

# NSG Rules for Load Balancer
resource "oci_core_network_security_group_security_rule" "lb_ingress_https" {
  network_security_group_id = oci_core_network_security_group.lb.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow HTTPS from internet"
  source      = "0.0.0.0/0"
  source_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 443
      max = 443
    }
  }
}

resource "oci_core_network_security_group_security_rule" "lb_ingress_http" {
  network_security_group_id = oci_core_network_security_group.lb.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow HTTP from internet (for redirect)"
  source      = "0.0.0.0/0"
  source_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 80
      max = 80
    }
  }
}

resource "oci_core_network_security_group_security_rule" "lb_egress_tmi" {
  network_security_group_id = oci_core_network_security_group.lb.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow traffic to TMI server"
  destination      = oci_core_network_security_group.tmi_server.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 8080
      max = 8080
    }
  }
}

# Network Security Group for Database
resource "oci_core_network_security_group" "database" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-database-nsg"

  freeform_tags = var.tags
}

# NSG Rules for Database
resource "oci_core_network_security_group_security_rule" "db_ingress_tmi" {
  network_security_group_id = oci_core_network_security_group.database.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow traffic from TMI server"
  source      = oci_core_network_security_group.tmi_server.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 1521
      max = 1522
    }
  }
}

# Network Security Group for TMI-UX (Frontend)
resource "oci_core_network_security_group" "tmi_ux" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-tmi-ux-nsg"

  freeform_tags = var.tags
}

# NSG Rules for TMI-UX
resource "oci_core_network_security_group_security_rule" "tmi_ux_ingress_lb" {
  network_security_group_id = oci_core_network_security_group.tmi_ux.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow traffic from load balancer"
  source      = oci_core_network_security_group.lb.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 8080
      max = 8080
    }
  }
}

resource "oci_core_network_security_group_security_rule" "tmi_ux_egress_https" {
  network_security_group_id = oci_core_network_security_group.tmi_ux.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow HTTPS to internet"
  destination      = "0.0.0.0/0"
  destination_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 443
      max = 443
    }
  }
}

# Allow load balancer to reach TMI-UX
resource "oci_core_network_security_group_security_rule" "lb_egress_tmi_ux" {
  network_security_group_id = oci_core_network_security_group.lb.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow traffic to TMI-UX server"
  destination      = oci_core_network_security_group.tmi_ux.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 8080
      max = 8080
    }
  }
}
