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

# Route Table (all subnets are private - no internet gateway)
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

# Security List for LB Subnet (internal only - NSGs handle most rules)
resource "oci_core_security_list" "public" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-lb-sl"

  # Allow HTTPS from within VCN (internal load balancer)
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.vcn_cidr
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 443
      max = 443
    }
  }

  # Allow HTTP from within VCN
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.vcn_cidr
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 80
      max = 80
    }
  }

  # Allow LB to reach containers in private subnet
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = var.private_subnet_cidr
    destination_type = "CIDR_BLOCK"
    tcp_options {
      min = 8080
      max = 8080
    }
  }

  # Allow LB to reach pods in OKE pod subnet (VCN-native pod networking)
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = var.oke_pod_subnet_cidr
    destination_type = "CIDR_BLOCK"
    tcp_options {
      min = 8080
      max = 8080
    }
  }

  freeform_tags = var.tags
}

# Security List for Private Subnet
resource "oci_core_security_list" "private" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-private-sl"

  # Allow traffic from LB (public subnet) to containers on port 8080
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.public_subnet_cidr
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 8080
      max = 8080
    }
  }

  # Allow LB to NodePort range (required for managed node pools)
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.public_subnet_cidr
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 30000
      max = 32767
    }
  }

  # Allow LB health check to kube-proxy (required for managed node pools)
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.public_subnet_cidr
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 10256
      max = 10256
    }
  }

  # Allow egress to database subnet (Oracle DB ports)
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = var.database_subnet_cidr
    destination_type = "CIDR_BLOCK"
    tcp_options {
      min = 1521
      max = 1522
    }
  }

  # Allow egress to OCI services (via service gateway)
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = data.oci_core_services.all_services.services[0].cidr_block
    destination_type = "SERVICE_CIDR_BLOCK"
    tcp_options {
      min = 443
      max = 443
    }
  }

  # Allow outbound HTTPS via NAT gateway (OAuth callbacks, external APIs)
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = "0.0.0.0/0"
    destination_type = "CIDR_BLOCK"
    tcp_options {
      min = 443
      max = 443
    }
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

  # Allow egress to OCI services only (for ADB internal operations)
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = data.oci_core_services.all_services.services[0].cidr_block
    destination_type = "SERVICE_CIDR_BLOCK"
    tcp_options {
      min = 443
      max = 443
    }
  }

  freeform_tags = var.tags
}

# LB Subnet (internal load balancers only, no public IPs)
resource "oci_core_subnet" "public" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.tmi.id
  cidr_block                 = var.public_subnet_cidr
  display_name               = "${var.name_prefix}-lb"
  dns_label                  = "public"
  route_table_id             = oci_core_route_table.private.id
  security_list_ids          = [oci_core_security_list.public.id]
  prohibit_public_ip_on_vnic = true

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

  description = "Allow HTTPS from within VCN"
  source      = var.vcn_cidr
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

  description = "Allow HTTP from within VCN"
  source      = var.vcn_cidr
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

# ============================================================================
# OKE (Oracle Kubernetes Engine) Network Resources
# ============================================================================

# Security List for OKE API Endpoint Subnet
resource "oci_core_security_list" "oke_api" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-oke-api-sl"

  # Allow Kubernetes API access
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.oke_public_endpoint ? "0.0.0.0/0" : var.vcn_cidr
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 6443
      max = 6443
    }
  }

  # Allow egress to VCN (API server to worker nodes)
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = var.vcn_cidr
    destination_type = "CIDR_BLOCK"
  }

  # Allow egress to OCI services (control plane operations)
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = data.oci_core_services.all_services.services[0].cidr_block
    destination_type = "SERVICE_CIDR_BLOCK"
    tcp_options {
      min = 443
      max = 443
    }
  }

  freeform_tags = var.tags
}

# Security List for OKE Pod Subnet
resource "oci_core_security_list" "oke_pod" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-oke-pod-sl"

  # Allow all traffic within pod subnet (pod-to-pod)
  ingress_security_rules {
    protocol    = "all"
    source      = var.oke_pod_subnet_cidr
    source_type = "CIDR_BLOCK"
  }

  # Allow traffic from OKE API to pods
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.oke_api_subnet_cidr
    source_type = "CIDR_BLOCK"
  }

  # Allow traffic from LB (public subnet) to pods on port 8080
  ingress_security_rules {
    protocol    = "6" # TCP
    source      = var.public_subnet_cidr
    source_type = "CIDR_BLOCK"
    tcp_options {
      min = 8080
      max = 8080
    }
  }

  # Allow all egress for pods
  egress_security_rules {
    protocol         = "all"
    destination      = "0.0.0.0/0"
    destination_type = "CIDR_BLOCK"
  }

  # Allow egress to OCI services
  egress_security_rules {
    protocol         = "6" # TCP
    destination      = data.oci_core_services.all_services.services[0].cidr_block
    destination_type = "SERVICE_CIDR_BLOCK"
    tcp_options {
      min = 443
      max = 443
    }
  }

  freeform_tags = var.tags
}

# OKE API Endpoint Subnet (private, no public IPs)
resource "oci_core_subnet" "oke_api" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.tmi.id
  cidr_block                 = var.oke_api_subnet_cidr
  display_name               = "${var.name_prefix}-oke-api"
  dns_label                  = "okeapi"
  route_table_id             = oci_core_route_table.private.id
  security_list_ids          = [oci_core_security_list.oke_api.id]
  prohibit_public_ip_on_vnic = !var.oke_public_endpoint

  freeform_tags = var.tags
}

# OKE Pod Subnet (private, for virtual node pods)
resource "oci_core_subnet" "oke_pod" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.tmi.id
  cidr_block                 = var.oke_pod_subnet_cidr
  display_name               = "${var.name_prefix}-oke-pod"
  dns_label                  = "okepod"
  route_table_id             = oci_core_route_table.private.id
  security_list_ids          = [oci_core_security_list.oke_pod.id]
  prohibit_public_ip_on_vnic = true

  freeform_tags = var.tags
}

# Network Security Group for OKE API Endpoint
resource "oci_core_network_security_group" "oke_api" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-oke-api-nsg"

  freeform_tags = var.tags
}

# NSG Rules for OKE API Endpoint
resource "oci_core_network_security_group_security_rule" "oke_api_ingress" {
  for_each                  = toset(var.oke_api_authorized_cidrs)
  network_security_group_id = oci_core_network_security_group.oke_api.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow Kubernetes API access from ${each.value}"
  source      = each.value
  source_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 6443
      max = 6443
    }
  }
}

resource "oci_core_network_security_group_security_rule" "oke_api_egress_pods" {
  network_security_group_id = oci_core_network_security_group.oke_api.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow API server to communicate with pods"
  destination      = oci_core_network_security_group.oke_pod.id
  destination_type = "NETWORK_SECURITY_GROUP"
}

resource "oci_core_network_security_group_security_rule" "oke_api_egress_services" {
  network_security_group_id = oci_core_network_security_group.oke_api.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow API server to access OCI services"
  destination      = data.oci_core_services.all_services.services[0].cidr_block
  destination_type = "SERVICE_CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 443
      max = 443
    }
  }
}

# Network Security Group for OKE Pods
resource "oci_core_network_security_group" "oke_pod" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.tmi.id
  display_name   = "${var.name_prefix}-oke-pod-nsg"

  freeform_tags = var.tags
}

# NSG Rules for OKE Pods
resource "oci_core_network_security_group_security_rule" "oke_pod_ingress_api" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow traffic from OKE API server"
  source      = oci_core_network_security_group.oke_api.id
  source_type = "NETWORK_SECURITY_GROUP"
}

resource "oci_core_network_security_group_security_rule" "oke_pod_ingress_pod" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "INGRESS"
  protocol                  = "all"

  description = "Allow pod-to-pod communication"
  source      = oci_core_network_security_group.oke_pod.id
  source_type = "NETWORK_SECURITY_GROUP"
}

resource "oci_core_network_security_group_security_rule" "oke_pod_ingress_lb" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
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

resource "oci_core_network_security_group_security_rule" "oke_pod_egress_pod" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "EGRESS"
  protocol                  = "all"

  description      = "Allow pod-to-pod communication"
  destination      = oci_core_network_security_group.oke_pod.id
  destination_type = "NETWORK_SECURITY_GROUP"
}

resource "oci_core_network_security_group_security_rule" "oke_pod_egress_db" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
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

resource "oci_core_network_security_group_security_rule" "oke_pod_egress_services" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
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

resource "oci_core_network_security_group_security_rule" "oke_pod_egress_internet" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow HTTPS to internet (OAuth callbacks, external APIs, image pulls)"
  destination      = "0.0.0.0/0"
  destination_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 443
      max = 443
    }
  }
}

# Worker node to OKE API endpoint (required for kubelet registration)
resource "oci_core_network_security_group_security_rule" "oke_pod_egress_api" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow worker nodes to reach OKE API endpoint"
  destination      = oci_core_network_security_group.oke_api.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 6443
      max = 6443
    }
  }
}

resource "oci_core_network_security_group_security_rule" "oke_pod_egress_api_12250" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow worker nodes to reach OKE API (control plane communication)"
  destination      = oci_core_network_security_group.oke_api.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 12250
      max = 12250
    }
  }
}

# ICMP for path MTU discovery
resource "oci_core_network_security_group_security_rule" "oke_pod_egress_icmp" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "EGRESS"
  protocol                  = "1" # ICMP

  description      = "ICMP for path MTU discovery"
  destination      = "0.0.0.0/0"
  destination_type = "CIDR_BLOCK"

  icmp_options {
    type = 3
    code = 4
  }
}

# OKE API ingress from worker nodes
resource "oci_core_network_security_group_security_rule" "oke_api_ingress_workers" {
  network_security_group_id = oci_core_network_security_group.oke_api.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow worker nodes to reach K8s API"
  source      = oci_core_network_security_group.oke_pod.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 6443
      max = 6443
    }
  }
}

resource "oci_core_network_security_group_security_rule" "oke_api_ingress_workers_12250" {
  network_security_group_id = oci_core_network_security_group.oke_api.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow worker nodes to reach OKE control plane"
  source      = oci_core_network_security_group.oke_pod.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 12250
      max = 12250
    }
  }
}

# Allow load balancer to reach OKE pods
resource "oci_core_network_security_group_security_rule" "lb_egress_oke_pods" {
  network_security_group_id = oci_core_network_security_group.lb.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow traffic to OKE pods"
  destination      = oci_core_network_security_group.oke_pod.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 8080
      max = 8080
    }
  }
}

# Allow load balancer to reach OKE NodePort range (required for managed node pools)
resource "oci_core_network_security_group_security_rule" "lb_egress_oke_nodeport" {
  network_security_group_id = oci_core_network_security_group.lb.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow traffic to OKE NodePort range (managed nodes)"
  destination      = oci_core_network_security_group.oke_pod.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 30000
      max = 32767
    }
  }
}

# Allow load balancer health check to kube-proxy (required for managed node pools)
resource "oci_core_network_security_group_security_rule" "lb_egress_oke_healthcheck" {
  network_security_group_id = oci_core_network_security_group.lb.id
  direction                 = "EGRESS"
  protocol                  = "6" # TCP

  description      = "Allow health check to kube-proxy (managed nodes)"
  destination      = oci_core_network_security_group.oke_pod.id
  destination_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 10256
      max = 10256
    }
  }
}

# Allow OKE workers to accept NodePort traffic from load balancer (managed nodes)
resource "oci_core_network_security_group_security_rule" "oke_pod_ingress_lb_nodeport" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow LB to NodePort range (managed nodes)"
  source      = oci_core_network_security_group.lb.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 30000
      max = 32767
    }
  }
}

# Allow OKE workers to accept health check from load balancer (managed nodes)
resource "oci_core_network_security_group_security_rule" "oke_pod_ingress_lb_healthcheck" {
  network_security_group_id = oci_core_network_security_group.oke_pod.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow LB health check to kube-proxy (managed nodes)"
  source      = oci_core_network_security_group.lb.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 10256
      max = 10256
    }
  }
}

# Allow OKE pods to reach database NSG
resource "oci_core_network_security_group_security_rule" "db_ingress_oke_pods" {
  network_security_group_id = oci_core_network_security_group.database.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  description = "Allow traffic from OKE pods"
  source      = oci_core_network_security_group.oke_pod.id
  source_type = "NETWORK_SECURITY_GROUP"

  tcp_options {
    destination_port_range {
      min = 1521
      max = 1522
    }
  }
}
