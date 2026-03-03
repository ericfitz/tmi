# OCI Compute Module for TMI
# Creates a VM.Standard.E5.Flex (AMD x86-64) instance in the public subnet
# with a direct public IP, running TMI Server + PostgreSQL + Redis via Podman

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

# Find the latest Oracle Linux 9 platform image compatible with E5.Flex (x86-64)
data "oci_core_images" "ol9_x86" {
  compartment_id           = var.compartment_id
  operating_system         = "Oracle Linux"
  operating_system_version = "9"
  shape                    = "VM.Standard.E5.Flex"
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"
  state                    = "AVAILABLE"
}

locals {
  availability_domain = var.availability_domain != null ? var.availability_domain : data.oci_identity_availability_domains.ads.availability_domains[0].name
}

# TMI Application Server VM (E5.Flex x86-64, runs TMI + PostgreSQL + Redis via Podman)
resource "oci_core_instance" "tmi" {
  compartment_id      = var.compartment_id
  availability_domain = local.availability_domain
  display_name        = "${var.name_prefix}-server"

  shape = "VM.Standard.E5.Flex"
  shape_config {
    ocpus         = var.vm_ocpus
    memory_in_gbs = var.vm_memory_gb
  }

  source_details {
    source_type             = "image"
    source_id               = data.oci_core_images.ol9_x86.images[0].id
    boot_volume_size_in_gbs = var.boot_volume_size_gb
  }

  create_vnic_details {
    subnet_id        = var.public_subnet_id
    nsg_ids          = distinct(concat(var.tmi_nsg_ids, var.redis_nsg_ids))
    assign_public_ip = true
    display_name     = "${var.name_prefix}-server-vnic"
  }

  metadata = {
    user_data = base64encode(templatefile("${path.module}/templates/cloud-init.yaml.tpl", {
      tmi_image_url             = var.tmi_image_url
      redis_image_url           = var.redis_docker_image
      postgres_image_url        = var.postgres_image_url
      tmi_ux_image_url          = var.tmi_ux_image_url
      tmi_ux_api_url            = var.tmi_ux_api_url
      postgres_password         = var.postgres_password
      postgres_password_encoded = urlencode(var.postgres_password)
      redis_password            = var.redis_password
      redis_password_encoded    = urlencode(var.redis_password)
      jwt_secret                = var.jwt_secret
      oauth_client_secret       = var.oauth_client_secret
      log_level                 = var.log_level
    }))
    ssh_authorized_keys = var.ssh_authorized_keys != null ? var.ssh_authorized_keys : ""
  }

  freeform_tags = var.tags

  # Prevent VM recreation when cloud-init template changes (use taint to force rebuild)
  lifecycle {
    ignore_changes = [metadata, source_details, availability_domain]
  }

  timeouts {
    create = "30m"
  }
}
