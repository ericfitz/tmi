# AWS Kubernetes (EKS) Module for TMI
# Creates an EKS cluster with Fargate (serverless, analogous to OKE virtual nodes)

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = ">= 4.0.0"
    }
    null = {
      source  = "hashicorp/null"
      version = ">= 3.0.0"
    }
  }
}

# =============================================================================
# IAM Roles
# =============================================================================

# IAM role for EKS cluster
resource "aws_iam_role" "eks_cluster" {
  name = "${var.name_prefix}-eks-cluster-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "eks.amazonaws.com"
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "eks_cluster_policy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
  role       = aws_iam_role.eks_cluster.name
}

# IAM role for Fargate pod execution
resource "aws_iam_role" "fargate" {
  name = "${var.name_prefix}-eks-fargate-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "eks-fargate-pods.amazonaws.com"
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "fargate_pod_execution" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSFargatePodExecutionRolePolicy"
  role       = aws_iam_role.fargate.name
}

# =============================================================================
# EKS Cluster
# =============================================================================

resource "aws_eks_cluster" "tmi" {
  name     = "${var.name_prefix}-eks"
  version  = var.kubernetes_version
  role_arn = aws_iam_role.eks_cluster.arn

  vpc_config {
    subnet_ids              = concat(var.private_subnet_ids, var.public_subnet_ids)
    security_group_ids      = var.eks_security_group_ids
    endpoint_private_access = true
    endpoint_public_access  = true
    public_access_cidrs     = var.authorized_cidrs
  }

  dynamic "encryption_config" {
    for_each = var.kms_key_arn != null ? [1] : []
    content {
      provider {
        key_arn = var.kms_key_arn
      }
      resources = ["secrets"]
    }
  }

  tags = var.tags

  depends_on = [
    aws_iam_role_policy_attachment.eks_cluster_policy,
  ]
}

# =============================================================================
# Fargate Profile
# =============================================================================

resource "aws_eks_fargate_profile" "tmi" {
  cluster_name           = aws_eks_cluster.tmi.name
  fargate_profile_name   = "${var.name_prefix}-tmi"
  pod_execution_role_arn = aws_iam_role.fargate.arn
  subnet_ids             = var.fargate_subnet_ids

  selector {
    namespace = "tmi"
  }

  tags = var.tags
}

# Fargate profile for kube-system namespace (required for CoreDNS on Fargate-only clusters).
# Without this, CoreDNS pods stay Pending and no pods can resolve DNS.
resource "aws_eks_fargate_profile" "kube_system" {
  cluster_name           = aws_eks_cluster.tmi.name
  fargate_profile_name   = "${var.name_prefix}-kube-system"
  pod_execution_role_arn = aws_iam_role.fargate.arn
  subnet_ids             = var.fargate_subnet_ids

  selector {
    namespace = "kube-system"
  }

  tags = var.tags
}

# Patch CoreDNS to remove the ec2 compute-type annotation so it schedules on Fargate.
# EKS defaults CoreDNS with eks.amazonaws.com/compute-type=ec2 which prevents
# scheduling on Fargate-only clusters.
resource "null_resource" "patch_coredns" {
  provisioner "local-exec" {
    command = <<-EOT
      aws eks update-kubeconfig --name ${aws_eks_cluster.tmi.name} --region ${var.aws_region} --kubeconfig /tmp/${var.name_prefix}-eks-kubeconfig
      kubectl --kubeconfig /tmp/${var.name_prefix}-eks-kubeconfig \
        patch deployment coredns -n kube-system --type json \
        -p='[{"op": "remove", "path": "/spec/template/metadata/annotations/eks.amazonaws.com~1compute-type"}]' \
        2>/dev/null || true
      kubectl --kubeconfig /tmp/${var.name_prefix}-eks-kubeconfig \
        rollout restart deployment coredns -n kube-system
      kubectl --kubeconfig /tmp/${var.name_prefix}-eks-kubeconfig \
        rollout status deployment coredns -n kube-system --timeout=300s
      rm -f /tmp/${var.name_prefix}-eks-kubeconfig
    EOT
  }

  depends_on = [
    aws_eks_fargate_profile.kube_system,
    aws_eks_cluster.tmi,
  ]
}

# =============================================================================
# OIDC Provider for IRSA (IAM Roles for Service Accounts)
# =============================================================================

data "tls_certificate" "eks" {
  url = aws_eks_cluster.tmi.identity[0].oidc[0].issuer
}

resource "aws_iam_openid_connect_provider" "eks" {
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.eks.certificates[0].sha1_fingerprint]
  url             = aws_eks_cluster.tmi.identity[0].oidc[0].issuer

  tags = var.tags
}

# IAM role for IRSA - allows TMI pods to access AWS services
resource "aws_iam_role" "tmi_irsa" {
  name = "${var.name_prefix}-tmi-irsa-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRoleWithWebIdentity"
        Effect = "Allow"
        Principal = {
          Federated = aws_iam_openid_connect_provider.eks.arn
        }
        Condition = {
          StringEquals = {
            "${replace(aws_eks_cluster.tmi.identity[0].oidc[0].issuer, "https://", "")}:sub" = "system:serviceaccount:tmi:tmi-api"
            "${replace(aws_eks_cluster.tmi.identity[0].oidc[0].issuer, "https://", "")}:aud" = "sts.amazonaws.com"
          }
        }
      }
    ]
  })

  tags = var.tags
}

# =============================================================================
# Cluster Auth (for kubernetes provider)
# =============================================================================

data "aws_eks_cluster_auth" "tmi" {
  name = aws_eks_cluster.tmi.name
}
