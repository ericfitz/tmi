# Outputs for AWS Kubernetes (EKS) Module

# EKS Cluster
output "cluster_id" {
  description = "ID of the EKS cluster"
  value       = aws_eks_cluster.tmi.id
}

output "cluster_name" {
  description = "Name of the EKS cluster"
  value       = aws_eks_cluster.tmi.name
}

output "cluster_endpoint" {
  description = "Kubernetes API endpoint of the EKS cluster"
  value       = aws_eks_cluster.tmi.endpoint
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate for the EKS cluster"
  value       = aws_eks_cluster.tmi.certificate_authority[0].data
  sensitive   = true
}

output "cluster_oidc_issuer_url" {
  description = "OIDC issuer URL for the EKS cluster"
  value       = aws_eks_cluster.tmi.identity[0].oidc[0].issuer
}

output "oidc_provider_arn" {
  description = "ARN of the OIDC provider for IRSA"
  value       = aws_iam_openid_connect_provider.eks.arn
}

# Node Group
output "node_group_id" {
  description = "ID of the managed node group"
  value       = aws_eks_node_group.tmi.id
}

output "node_role_arn" {
  description = "ARN of the EKS node IAM role"
  value       = aws_iam_role.eks_nodes.arn
}

# TMI Pod Role
output "tmi_pod_role_arn" {
  description = "ARN of the TMI pod IAM role (IRSA)"
  value       = aws_iam_role.tmi_pod.arn
}

# Kubernetes config command
output "kubernetes_config_command" {
  description = "Command to configure kubectl for this cluster"
  value       = "aws eks update-kubeconfig --region ${data.aws_region.current.name} --name ${aws_eks_cluster.tmi.name}"
}

# Namespace
output "namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = kubernetes_namespace_v1.tmi.metadata[0].name
}

# Standard interface outputs for multi-cloud compatibility
#
# service_endpoint / load_balancer_dns: the ALB is now created by the AWS
# Load Balancer Controller from the Ingress in the deployments/k8s/dev/aws
# overlay (Task 5), not by a terraform-managed kubernetes_ingress_v1
# resource, so its hostname is not known to terraform. These outputs are
# kept (returning null) only because aws-private/outputs.tf and
# aws-public/outputs.tf reference module.kubernetes.service_endpoint; the
# deploy script resolves the real ALB hostname post-apply (`kubectl get
# ingress`) and uses it to create/update the Route 53 CNAME.
output "service_endpoint" {
  description = "Service endpoint URL (standard interface). Always null — the ALB Ingress is owned by the deploy overlay, not terraform; resolve the real hostname with `kubectl get ingress` after the overlay is applied."
  value       = null
}

output "load_balancer_dns" {
  description = "Load balancer DNS name (standard interface). Always null — see service_endpoint."
  value       = null
}

output "redis_endpoint" {
  description = "Internal Redis service address. This is the DNS name the deploy overlay's Redis Service (deployments/k8s/dev/redis.yml, Service \"redis\" in this namespace) will resolve to — it is not backed by a terraform-managed Service."
  value       = "redis.${kubernetes_namespace_v1.tmi.metadata[0].name}.svc.cluster.local:6379"
}
