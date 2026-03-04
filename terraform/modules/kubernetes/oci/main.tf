# OCI Kubernetes (OKE) Module for TMI
# Creates an OKE Enhanced Cluster with Virtual Node Pool
# Replaces the compute/oci module (Container Instances + Load Balancer)

terraform {
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
  }
}

# Data source for availability domains
data "oci_identity_availability_domains" "ads" {
  compartment_id = var.compartment_id
}

locals {
  availability_domain = var.availability_domain != null ? var.availability_domain : data.oci_identity_availability_domains.ads.availability_domains[0].name
}

# OKE Enhanced Cluster
resource "oci_containerengine_cluster" "tmi" {
  compartment_id     = var.compartment_id
  kubernetes_version = var.kubernetes_version
  name               = "${var.name_prefix}-oke"
  vcn_id             = var.vcn_id
  type               = "ENHANCED_CLUSTER"

  endpoint_config {
    is_public_ip_enabled = true
    subnet_id            = var.oke_api_subnet_id
    nsg_ids              = var.oke_api_nsg_ids
  }

  cluster_pod_network_options {
    cni_type = "OCI_VCN_IP_NATIVE"
  }

  options {
    service_lb_subnet_ids = var.public_subnet_ids

    kubernetes_network_config {
      services_cidr = var.k8s_services_cidr
      pods_cidr     = var.k8s_pods_cidr
    }
  }

  freeform_tags = var.tags
}

# Virtual Node Pool (serverless pods - no node management required)
resource "oci_containerengine_virtual_node_pool" "tmi" {
  cluster_id     = oci_containerengine_cluster.tmi.id
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-virtual-pool"

  pod_configuration {
    shape     = var.virtual_node_pod_shape
    subnet_id = var.oke_pod_subnet_id
    nsg_ids   = var.oke_pod_nsg_ids
  }

  placement_configurations {
    availability_domain = local.availability_domain
    subnet_id           = var.oke_pod_subnet_id
    fault_domain        = ["FAULT-DOMAIN-1", "FAULT-DOMAIN-2", "FAULT-DOMAIN-3"]
  }

  size = var.virtual_node_count

  freeform_tags = var.tags
}

# Data source to get cluster endpoint and CA certificate for kubernetes provider
data "oci_containerengine_cluster_kube_config" "tmi" {
  cluster_id = oci_containerengine_cluster.tmi.id
}
