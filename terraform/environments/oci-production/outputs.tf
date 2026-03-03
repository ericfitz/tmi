# Outputs for TMI OCI Free Development Deployment

# Application Access
output "vm_public_ip" {
  description = "Public IP address of the TMI VM"
  value       = module.compute.vm_public_ip
}

output "application_url" {
  description = "URL to access the TMI-UX frontend"
  value       = module.compute.application_url
}

output "api_url" {
  description = "URL to access the TMI API"
  value       = module.compute.api_url
}

# Network Information
output "vcn_id" {
  description = "OCID of the VCN"
  value       = module.network.vcn_id
}

output "public_subnet_id" {
  description = "OCID of the public subnet"
  value       = module.network.public_subnet_id
}

output "private_subnet_id" {
  description = "OCID of the private subnet"
  value       = module.network.private_subnet_id
}

# VM Instance Information
output "tmi_instance_id" {
  description = "OCID of the TMI VM instance"
  value       = module.compute.tmi_instance_id
}

output "tmi_instance_state" {
  description = "State of the TMI VM instance"
  value       = module.compute.tmi_instance_state
}

# Generated Passwords (for initial setup - store securely!)
output "generated_passwords" {
  description = "Generated passwords (only shown if not provided)"
  sensitive   = true
  value = {
    postgres_password   = var.postgres_password == null ? "Generated - check terraform.tfstate" : "User provided"
    redis_password      = var.redis_password == null ? "Generated - check terraform.tfstate" : "User provided"
    jwt_secret          = var.jwt_secret == null ? "Generated - check terraform.tfstate" : "User provided"
    oauth_client_secret = var.oauth_client_secret == null ? "Generated - check terraform.tfstate" : "User provided"
  }
}

# Useful Commands
output "useful_commands" {
  description = "Useful commands for managing the deployment"
  value = {
    ssh_to_vm          = "ssh opc@${module.compute.vm_public_ip}"
    check_containers   = "ssh opc@${module.compute.vm_public_ip} 'sudo podman ps'"
    setup_log          = "ssh opc@${module.compute.vm_public_ip} 'sudo tail -f /var/log/tmi-setup.log'"
    set_oauth_callback = "ssh opc@${module.compute.vm_public_ip} \"echo 'TMI_OAUTH_CALLBACK_URL=http://${module.compute.vm_public_ip}:8080/oauth2/callback' | sudo tee -a /etc/tmi/tmi.env && sudo systemctl restart tmi-server\""
  }
}
