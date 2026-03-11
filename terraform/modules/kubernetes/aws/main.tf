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
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12"
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

# Attach CloudWatch Logs policy to Fargate role (required for Fargate log router)
resource "aws_iam_role_policy_attachment" "fargate_cloudwatch_logs" {
  count      = var.logging_policy_arn != null ? 1 : 0
  policy_arn = var.logging_policy_arn
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
# Fargate Log Router (sends pod stdout/stderr to CloudWatch)
# EKS Fargate has a built-in Fluent Bit log router that must be enabled via
# an aws-observability namespace and ConfigMap. Without this, all container
# stdout/stderr is discarded.
# See: https://docs.aws.amazon.com/eks/latest/userguide/fargate-logging.html
# =============================================================================

# Fargate profile for aws-observability namespace (required for log router)
resource "aws_eks_fargate_profile" "aws_observability" {
  count                  = var.cloudwatch_log_group != null ? 1 : 0
  cluster_name           = aws_eks_cluster.tmi.name
  fargate_profile_name   = "${var.name_prefix}-aws-observability"
  pod_execution_role_arn = aws_iam_role.fargate.arn
  subnet_ids             = var.fargate_subnet_ids

  selector {
    namespace = "aws-observability"
  }

  tags = var.tags
}

resource "kubernetes_namespace_v1" "aws_observability" {
  count = var.cloudwatch_log_group != null ? 1 : 0

  metadata {
    name = "aws-observability"
    labels = {
      aws-observability = "enabled"
    }
  }

  depends_on = [aws_eks_fargate_profile.tmi, null_resource.patch_coredns]
}

# ConfigMap that configures the Fargate built-in Fluent Bit log router.
# This routes all container stdout/stderr from Fargate pods to CloudWatch.
resource "kubernetes_config_map_v1" "aws_logging" {
  count = var.cloudwatch_log_group != null ? 1 : 0

  metadata {
    name      = "aws-logging"
    namespace = kubernetes_namespace_v1.aws_observability[0].metadata[0].name
  }

  data = {
    "output.conf" = <<-EOT
      [OUTPUT]
          Name              cloudwatch_logs
          Match             *
          region            ${var.aws_region}
          log_group_name    ${var.cloudwatch_log_group}
          log_stream_prefix fargate/
          auto_create_group false
    EOT

    "filters.conf" = <<-EOT
      [FILTER]
          Name   parser
          Match  *
          Key_Name log
          Parser  json
          Reserve_Data On
    EOT

    "parsers.conf" = <<-EOT
      [PARSER]
          Name   json
          Format json
          Time_Key time
          Time_Format %Y-%m-%dT%H:%M:%S.%LZ
          Time_Keep On
    EOT
  }
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

# =============================================================================
# AWS Load Balancer Controller
# Required for ALB Ingress with TLS termination, HTTP→HTTPS redirect,
# host-based routing, and WebSocket support.
# =============================================================================

# IAM policy for the AWS Load Balancer Controller
# Uses the official AWS-maintained policy document
data "aws_iam_policy_document" "aws_lb_controller" {
  statement {
    effect = "Allow"
    actions = [
      "iam:CreateServiceLinkedRole",
    ]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "iam:AWSServiceName"
      values   = ["elasticloadbalancing.amazonaws.com"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:DescribeAccountAttributes",
      "ec2:DescribeAddresses",
      "ec2:DescribeAvailabilityZones",
      "ec2:DescribeInternetGateways",
      "ec2:DescribeVpcs",
      "ec2:DescribeVpcPeeringConnections",
      "ec2:DescribeSubnets",
      "ec2:DescribeSecurityGroups",
      "ec2:DescribeInstances",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DescribeTags",
      "ec2:DescribeCoipPools",
      "ec2:GetCoipPoolUsage",
      "elasticloadbalancing:DescribeLoadBalancers",
      "elasticloadbalancing:DescribeLoadBalancerAttributes",
      "elasticloadbalancing:DescribeListeners",
      "elasticloadbalancing:DescribeListenerCertificates",
      "elasticloadbalancing:DescribeSSLPolicies",
      "elasticloadbalancing:DescribeRules",
      "elasticloadbalancing:DescribeTargetGroups",
      "elasticloadbalancing:DescribeTargetGroupAttributes",
      "elasticloadbalancing:DescribeTargetHealth",
      "elasticloadbalancing:DescribeTags",
      "elasticloadbalancing:DescribeTrustStores",
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "cognito-idp:DescribeUserPoolClient",
      "acm:ListCertificates",
      "acm:DescribeCertificate",
      "iam:ListServerCertificates",
      "iam:GetServerCertificate",
      "waf-regional:GetWebACL",
      "waf-regional:GetWebACLForResource",
      "waf-regional:AssociateWebACL",
      "waf-regional:DisassociateWebACL",
      "wafv2:GetWebACL",
      "wafv2:GetWebACLForResource",
      "wafv2:AssociateWebACL",
      "wafv2:DisassociateWebACL",
      "shield:GetSubscriptionState",
      "shield:DescribeProtection",
      "shield:CreateProtection",
      "shield:DeleteProtection",
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:AuthorizeSecurityGroupIngress",
      "ec2:RevokeSecurityGroupIngress",
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:CreateSecurityGroup",
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:CreateTags",
    ]
    resources = ["arn:aws:ec2:*:*:security-group/*"]
    condition {
      test     = "StringEquals"
      variable = "ec2:CreateAction"
      values   = ["CreateSecurityGroup"]
    }
    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:CreateTags",
      "ec2:DeleteTags",
    ]
    resources = ["arn:aws:ec2:*:*:security-group/*"]
    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["true"]
    }
    condition {
      test     = "Null"
      variable = "aws:ResourceTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:AuthorizeSecurityGroupIngress",
      "ec2:RevokeSecurityGroupIngress",
      "ec2:DeleteSecurityGroup",
    ]
    resources = ["*"]
    condition {
      test     = "Null"
      variable = "aws:ResourceTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:CreateLoadBalancer",
      "elasticloadbalancing:CreateTargetGroup",
    ]
    resources = ["*"]
    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:CreateListener",
      "elasticloadbalancing:DeleteListener",
      "elasticloadbalancing:CreateRule",
      "elasticloadbalancing:DeleteRule",
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:AddTags",
      "elasticloadbalancing:RemoveTags",
    ]
    resources = [
      "arn:aws:elasticloadbalancing:*:*:targetgroup/*/*",
      "arn:aws:elasticloadbalancing:*:*:loadbalancer/net/*/*",
      "arn:aws:elasticloadbalancing:*:*:loadbalancer/app/*/*",
    ]
    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["true"]
    }
    condition {
      test     = "Null"
      variable = "aws:ResourceTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:AddTags",
      "elasticloadbalancing:RemoveTags",
    ]
    resources = [
      "arn:aws:elasticloadbalancing:*:*:listener/net/*/*/*",
      "arn:aws:elasticloadbalancing:*:*:listener/app/*/*/*",
      "arn:aws:elasticloadbalancing:*:*:listener-rule/net/*/*/*",
      "arn:aws:elasticloadbalancing:*:*:listener-rule/app/*/*/*",
    ]
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:ModifyLoadBalancerAttributes",
      "elasticloadbalancing:SetIpAddressType",
      "elasticloadbalancing:SetSecurityGroups",
      "elasticloadbalancing:SetSubnets",
      "elasticloadbalancing:DeleteLoadBalancer",
      "elasticloadbalancing:ModifyTargetGroup",
      "elasticloadbalancing:ModifyTargetGroupAttributes",
      "elasticloadbalancing:DeleteTargetGroup",
    ]
    resources = ["*"]
    condition {
      test     = "Null"
      variable = "aws:ResourceTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:AddTags",
    ]
    resources = [
      "arn:aws:elasticloadbalancing:*:*:targetgroup/*/*",
      "arn:aws:elasticloadbalancing:*:*:loadbalancer/net/*/*",
      "arn:aws:elasticloadbalancing:*:*:loadbalancer/app/*/*",
    ]
    condition {
      test     = "StringEquals"
      variable = "elasticloadbalancing:CreateAction"
      values = [
        "CreateTargetGroup",
        "CreateLoadBalancer",
      ]
    }
    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:RegisterTargets",
      "elasticloadbalancing:DeregisterTargets",
    ]
    resources = ["arn:aws:elasticloadbalancing:*:*:targetgroup/*/*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:SetWebAcl",
      "elasticloadbalancing:ModifyListener",
      "elasticloadbalancing:AddListenerCertificates",
      "elasticloadbalancing:RemoveListenerCertificates",
      "elasticloadbalancing:ModifyRule",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_policy" "aws_lb_controller" {
  name   = "${var.name_prefix}-aws-lb-controller-policy"
  policy = data.aws_iam_policy_document.aws_lb_controller.json

  tags = var.tags
}

# IRSA role for the AWS Load Balancer Controller
resource "aws_iam_role" "aws_lb_controller" {
  name = "${var.name_prefix}-aws-lb-controller-role"

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
            "${replace(aws_eks_cluster.tmi.identity[0].oidc[0].issuer, "https://", "")}:sub" = "system:serviceaccount:kube-system:aws-load-balancer-controller"
            "${replace(aws_eks_cluster.tmi.identity[0].oidc[0].issuer, "https://", "")}:aud" = "sts.amazonaws.com"
          }
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "aws_lb_controller" {
  policy_arn = aws_iam_policy.aws_lb_controller.arn
  role       = aws_iam_role.aws_lb_controller.name
}

# Service account for the AWS Load Balancer Controller
resource "kubernetes_service_account_v1" "aws_lb_controller" {
  metadata {
    name      = "aws-load-balancer-controller"
    namespace = "kube-system"
    labels = {
      "app.kubernetes.io/component" = "controller"
      "app.kubernetes.io/name"      = "aws-load-balancer-controller"
    }
    annotations = {
      "eks.amazonaws.com/role-arn" = aws_iam_role.aws_lb_controller.arn
    }
  }

  depends_on = [aws_eks_fargate_profile.kube_system, null_resource.patch_coredns]
}

# Install the AWS Load Balancer Controller via Helm
resource "helm_release" "aws_lb_controller" {
  name       = "aws-load-balancer-controller"
  repository = "https://aws.github.io/eks-charts"
  chart      = "aws-load-balancer-controller"
  version    = "1.7.2"
  namespace  = "kube-system"

  set {
    name  = "clusterName"
    value = aws_eks_cluster.tmi.name
  }

  set {
    name  = "serviceAccount.create"
    value = "false"
  }

  set {
    name  = "serviceAccount.name"
    value = kubernetes_service_account_v1.aws_lb_controller.metadata[0].name
  }

  set {
    name  = "region"
    value = var.aws_region
  }

  set {
    name  = "vpcId"
    value = var.vpc_id
  }

  # Enable WAFv2 (future-proofing)
  set {
    name  = "enableWaf"
    value = "false"
  }

  set {
    name  = "enableWafv2"
    value = "false"
  }

  depends_on = [
    kubernetes_service_account_v1.aws_lb_controller,
    aws_iam_role_policy_attachment.aws_lb_controller,
    aws_eks_fargate_profile.kube_system,
    null_resource.patch_coredns,
  ]
}
