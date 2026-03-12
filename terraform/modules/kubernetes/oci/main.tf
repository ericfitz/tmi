# OCI Kubernetes (OKE) Module for TMI
# Creates an OKE Enhanced Cluster with Managed Node Pool

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

# Managed Node Pool
resource "oci_containerengine_node_pool" "tmi" {
  cluster_id         = oci_containerengine_cluster.tmi.id
  compartment_id     = var.compartment_id
  kubernetes_version = var.kubernetes_version
  name               = "${var.name_prefix}-node-pool"

  node_shape = var.node_shape

  node_shape_config {
    ocpus         = var.node_ocpus
    memory_in_gbs = var.node_memory_gbs
  }

  node_config_details {
    size = var.node_count

    placement_configs {
      availability_domain = local.availability_domain
      subnet_id           = var.oke_worker_subnet_id
    }

    nsg_ids                             = var.oke_pod_nsg_ids
    is_pv_encryption_in_transit_enabled = false

    # Required for VCN-native pod networking (OCI_VCN_IP_NATIVE)
    node_pool_pod_network_option_details {
      cni_type          = "OCI_VCN_IP_NATIVE"
      pod_subnet_ids    = [var.oke_pod_subnet_id]
      pod_nsg_ids       = var.oke_pod_nsg_ids
      max_pods_per_node = 31
    }
  }

  node_source_details {
    source_type = "IMAGE"
    image_id    = var.node_image_id
  }

  freeform_tags = var.tags
}

# Data source to get cluster endpoint and CA certificate for kubernetes provider
data "oci_containerengine_cluster_kube_config" "tmi" {
  cluster_id = oci_containerengine_cluster.tmi.id
}
