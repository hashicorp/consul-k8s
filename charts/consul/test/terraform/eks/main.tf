# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    aws = {
      version = "~> 5.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.27.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12.0"
    }
  }
}

provider "aws" {
  region = var.region

  dynamic "assume_role" {
    for_each = var.role_arn != "" ? [1] : []
    content {
      role_arn = var.role_arn
      duration = "2700s"
    }
  }
}

resource "random_id" "suffix" {
  count       = var.cluster_count
  byte_length = 4
}

data "aws_availability_zones" "available" {}

data "aws_caller_identity" "caller" {}

data "aws_ami" "hc-base-ubuntu-2404" {
  for_each = toset(["amd64", "arm64"])

  filter {
    name   = "name"
    values = [format("hc-base-ubuntu-2404-%s-*", each.value)]
  }

  filter {
    name   = "state"
    values = ["available"]
  }

  most_recent = true
  owners      = ["888995627335"] # ami-prod account
}

resource "random_string" "suffix" {
  length  = 8
  special = false
}

module "vpc" {
  count   = var.cluster_count
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"

  name = "consul-k8s-${random_id.suffix[count.index].dec}"
  # The cidr range needs to be unique in each VPC to allow setting up a peering connection.
  cidr                 = format("10.%s.0.0/16", count.index)
  azs                  = data.aws_availability_zones.available.names
  private_subnets      = [format("10.%s.1.0/24", count.index), format("10.%s.2.0/24", count.index), format("10.%s.3.0/24", count.index)]
  public_subnets       = [format("10.%s.4.0/24", count.index), format("10.%s.5.0/24", count.index), format("10.%s.6.0/24", count.index)]
  enable_nat_gateway   = true
  single_nat_gateway   = true
  enable_dns_hostnames = true

  # Enable dual-stack (IPv4 + IPv6) support for acceptance tests
  # enable_ipv6                  = true
  # public_subnet_ipv6_prefixes  = [0, 1, 2]
  # private_subnet_ipv6_prefixes = [3, 4, 5]

  public_subnet_tags = {
    "kubernetes.io/cluster/consul-k8s-${random_id.suffix[count.index].dec}" = "shared"
    "kubernetes.io/role/elb"                                                = "1"
  }

  private_subnet_tags = {
    "kubernetes.io/cluster/consul-k8s-${random_id.suffix[count.index].dec}" = "shared"
    "kubernetes.io/role/internal-elb"                                       = "1"
  }

  tags = var.tags
}

module "eks" {
  count = var.cluster_count

  source                 = "terraform-aws-modules/eks/aws"
  version                = "17.24.0"
  kubeconfig_api_version = "client.authentication.k8s.io/v1beta1"

  cluster_name    = "consul-k8s-${random_id.suffix[count.index].dec}"
  cluster_version = var.kubernetes_version
  subnets         = module.vpc[count.index].private_subnets
  enable_irsa     = true

  vpc_id = module.vpc[count.index].vpc_id

  node_groups = {
    first = {
      desired_capacity = 3
      max_capacity     = 3
      min_capacity     = 3

      instance_types = ["m5.xlarge"]
    }
  }

  manage_aws_auth        = false
  write_kubeconfig       = true
  kubeconfig_output_path = pathexpand("~/.kube/consul-k8s-${random_id.suffix[count.index].dec}")

  tags = var.tags
}

# Multi cluster setup.
# K8s Provider for the first cluster (cluster0)
provider "kubernetes" {
  alias                  = "cluster0"
  host                   = module.eks[0].cluster_endpoint
  cluster_ca_certificate = base64decode(module.eks[0].cluster_certificate_authority_data)
  token                  = data.aws_eks_cluster_auth.cluster[0].token
}

# Provider for second cluster (cluster1)
provider "kubernetes" {
  alias = "cluster1"

  # Use null to disable the provider configuration if cluster_count is not > 1
  # This avoids errors from empty string credentials.
  host                   = var.cluster_count > 1 ? module.eks[1].cluster_endpoint : null
  cluster_ca_certificate = var.cluster_count > 1 ? base64decode(module.eks[1].cluster_certificate_authority_data) : null
  token                  = var.cluster_count > 1 ? data.aws_eks_cluster_auth.cluster[1].token : null
}


resource "aws_iam_role" "csi-driver-role" {
  count = var.cluster_count
  name  = "consul-k8s-csi-role-${random_id.suffix[count.index].dec}"
  assume_role_policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Effect = "Allow",
        Action = "sts:AssumeRoleWithWebIdentity",
        Principal = {
          Federated = module.eks[count.index].oidc_provider_arn
        },
        Condition = {
          StringEquals = {
            join(":", [trimprefix(module.eks[count.index].cluster_oidc_issuer_url, "https://"), "aud"]) = ["sts.amazonaws.com"],
            join(":", [trimprefix(module.eks[count.index].cluster_oidc_issuer_url, "https://"), "sub"]) = ["system:serviceaccount:kube-system:ebs-csi-controller-sa"],
          }
        }
      }
    ]
  })
}

data "aws_iam_policy" "csi-driver-policy" {
  name = "AmazonEBSCSIDriverPolicy"
}

resource "aws_iam_role_policy_attachment" "csi" {
  count      = var.cluster_count
  role       = aws_iam_role.csi-driver-role[count.index].name
  policy_arn = data.aws_iam_policy.csi-driver-policy.arn
}

resource "aws_eks_addon" "csi-driver" {
  count                       = var.cluster_count
  cluster_name                = module.eks[count.index].cluster_id
  addon_name                  = "aws-ebs-csi-driver"
  addon_version               = "v1.44.0-eksbuild.1"
  service_account_role_arn    = aws_iam_role.csi-driver-role[count.index].arn
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "OVERWRITE"
}

data "aws_eks_cluster" "cluster" {
  count = var.cluster_count
  name  = module.eks[count.index].cluster_id
}

data "aws_eks_cluster_auth" "cluster" {
  count = var.cluster_count
  name  = module.eks[count.index].cluster_id
}

# Add a default StorageClass for dynamic volume provisioning
# This is the primary fix for the "unbound PersistentVolumeClaims" issue 
# as we do not specify storage class in default helm values.yaml for consul server.

# StorageClass for the first cluster (cluster0)
resource "kubernetes_storage_class" "ebs_gp3_cluster0" {
  provider   = kubernetes.cluster0
  depends_on = [module.eks, aws_eks_addon.csi-driver[0]]

  metadata {
    name = "gp3"
    annotations = {
      "storageclass.kubernetes.io/is-default-class" = "true"
    }
  }
  storage_provisioner = "ebs.csi.aws.com"
  parameters = {
    type      = "gp3"
    encrypted = "true"
  }
  reclaim_policy      = "Delete"
  volume_binding_mode = "WaitForFirstConsumer"
}

# StorageClass for second cluster (cluster1)
resource "kubernetes_storage_class" "ebs_gp3_cluster1" {
  count = var.cluster_count > 1 ? 1 : 0

  provider   = kubernetes.cluster1
  depends_on = [module.eks, aws_eks_addon.csi-driver[1]]
  metadata {
    name = "gp3"
    annotations = {
      "storageclass.kubernetes.io/is-default-class" = "true"
    }
  }
  storage_provisioner = "ebs.csi.aws.com"
  parameters = {
    type      = "gp3"
    encrypted = "true"
  }
  reclaim_policy      = "Delete"
  volume_binding_mode = "WaitForFirstConsumer"
}

# The following resources are only applied when cluster_count=2 to set up vpc peering and the appropriate routes and
# security groups so traffic between VPCs is allowed. There is validation to ensure cluster_count can be 1 or 2.

# Each EKS cluster needs to allow ingress traffic from the other VPC's public subnets.
# Traffic routes via NAT Gateway through public subnets (no private peering routes),
# so source IPs arrive from the public subnet CIDRs of the remote VPC.
resource "aws_security_group_rule" "allowingressfrom1-0" {
  count             = var.cluster_count > 1 ? length(module.vpc[1].public_subnets_cidr_blocks) : 0
  type              = "ingress"
  from_port         = 0
  to_port           = 0
  protocol          = "-1"
  cidr_blocks       = [module.vpc[1].public_subnets_cidr_blocks[count.index]]
  security_group_id = module.eks[0].worker_security_group_id
  description       = "Allow node traffic from cluster 1 public subnet ${count.index}"
}

resource "aws_security_group_rule" "allowingressfrom0-1" {
  count             = var.cluster_count > 1 ? length(module.vpc[0].public_subnets_cidr_blocks) : 0
  type              = "ingress"
  from_port         = 0
  to_port           = 0
  protocol          = "-1"
  cidr_blocks       = [module.vpc[0].public_subnets_cidr_blocks[count.index]]
  security_group_id = module.eks[1].worker_security_group_id
  description       = "Allow node traffic from cluster 0 public subnet ${count.index}"
}

# Create a peering connection. This is the requester's side of the connection.
resource "aws_vpc_peering_connection" "peer" {
  count         = var.cluster_count > 1 ? 1 : 0
  vpc_id        = module.vpc[0].vpc_id
  peer_vpc_id   = module.vpc[1].vpc_id
  peer_owner_id = data.aws_caller_identity.caller.account_id
  peer_region   = var.region
  auto_accept   = false

  tags = {
    Side = "Requester"
  }
}

# Accepter's side of the vpc peering connection.
resource "aws_vpc_peering_connection_accepter" "peer" {
  count                     = var.cluster_count > 1 ? 1 : 0
  vpc_peering_connection_id = aws_vpc_peering_connection.peer[0].id
  auto_accept               = true

  tags = {
    Side = "Accepter"
  }
}

# Add routes to public route tables in VPC 0 to route traffic to VPC 1 through the peering connection.
resource "aws_route" "peering_public_0" {
  count                     = var.cluster_count > 1 ? length(module.vpc[0].public_route_table_ids) : 0
  route_table_id            = module.vpc[0].public_route_table_ids[count.index]
  destination_cidr_block    = module.vpc[1].vpc_cidr_block
  vpc_peering_connection_id = aws_vpc_peering_connection.peer[0].id
}

# Add routes to public route tables in VPC 1 to route traffic to VPC 0 through the peering connection.
resource "aws_route" "peering_public_1" {
  count                     = var.cluster_count > 1 ? length(module.vpc[1].public_route_table_ids) : 0
  route_table_id            = module.vpc[1].public_route_table_ids[count.index]
  destination_cidr_block    = module.vpc[0].vpc_cidr_block
  vpc_peering_connection_id = aws_vpc_peering_connection.peer[0].id
}

# Deploy IBM Uptycs EDR agent to each EKS cluster.
provider "helm" {
  alias = "cluster_0"
  kubernetes {
    host                   = module.eks[0].cluster_endpoint
    cluster_ca_certificate = base64decode(module.eks[0].cluster_certificate_authority_data)
    token                  = data.aws_eks_cluster_auth.cluster[0].token
  }
}

provider "helm" {
  alias = "cluster_1"
  kubernetes {
    host                   = var.cluster_count > 1 ? module.eks[1].cluster_endpoint : module.eks[0].cluster_endpoint
    cluster_ca_certificate = base64decode(var.cluster_count > 1 ? module.eks[1].cluster_certificate_authority_data : module.eks[0].cluster_certificate_authority_data)
    token                  = var.cluster_count > 1 ? data.aws_eks_cluster_auth.cluster[1].token : data.aws_eks_cluster_auth.cluster[0].token
  }
}

# Cluster 0 EDR
resource "helm_release" "uptycs_0" {
  provider         = helm.cluster_0
  name             = "k8sosquery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "k8sosquery"
  namespace        = "uptycs"
  create_namespace = true
  cleanup_on_fail  = true

  values = [
    templatefile("${path.module}/k8sosquery-values.yaml", {
      owner         = var.uptycs_owner
      enroll_secret = var.uptycs_enroll_secret
    })
  ]
}

resource "helm_release" "kubequery_0" {
  depends_on       = [helm_release.uptycs_0]
  provider         = helm.cluster_0
  name             = "kubequery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "kubequery"
  namespace        = "kubequery"
  create_namespace = true
  cleanup_on_fail  = true

  set {
    name  = "deployment.spec.hostname"
    value = module.eks[0].cluster_id
  }

  values = [
    templatefile("${path.module}/kubequery-values.yaml", {
      enroll_secret     = var.uptycs_enroll_secret
      webhook_ca_bundle = var.uptycs_webhook_ca_bundle
      webhook_tls_crt   = var.uptycs_webhook_tls_crt
      webhook_tls_key   = var.uptycs_webhook_tls_key
    })
  ]
}

# Cluster 1 EDR (only when cluster_count > 1)
resource "helm_release" "uptycs_1" {
  count            = var.cluster_count > 1 ? 1 : 0
  provider         = helm.cluster_1
  name             = "k8sosquery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "k8sosquery"
  namespace        = "uptycs"
  create_namespace = true
  cleanup_on_fail  = true

  values = [
    templatefile("${path.module}/k8sosquery-values.yaml", {
      owner         = var.uptycs_owner
      enroll_secret = var.uptycs_enroll_secret
    })
  ]
}

resource "helm_release" "kubequery_1" {
  count            = var.cluster_count > 1 ? 1 : 0
  depends_on       = [helm_release.uptycs_1]
  provider         = helm.cluster_1
  name             = "kubequery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "kubequery"
  namespace        = "kubequery"
  create_namespace = true
  cleanup_on_fail  = true

  set {
    name  = "deployment.spec.hostname"
    value = module.eks[1].cluster_id
  }

  values = [
    templatefile("${path.module}/kubequery-values.yaml", {
      enroll_secret     = var.uptycs_enroll_secret
      webhook_ca_bundle = var.uptycs_webhook_ca_bundle
      webhook_tls_crt   = var.uptycs_webhook_tls_crt
      webhook_tls_key   = var.uptycs_webhook_tls_key
    })
  ]
}
