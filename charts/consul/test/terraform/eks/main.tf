# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    aws = {
      version = "~> 5.0"
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

      instance_type = "m5.xlarge"
    }
  }

  manage_aws_auth        = false
  write_kubeconfig       = true
  kubeconfig_output_path = pathexpand("~/.kube/consul-k8s-${random_id.suffix[count.index].dec}")

  tags = var.tags
}

resource "aws_iam_role" "csi-driver-role" {
  count = var.cluster_count
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
# as we do not specify storage class in default helm values.yaml.
resource "kubernetes_storage_class" "ebs_gp3" {
  metadata {
    name = "gp3"
    annotations = {
      "storageclass.kubernetes.io/is-default-class" = "true"
    }
  }
  storage_provisioner = "ebs.csi.aws.com"
  parameters = {
    type = "gp3"
  }
  reclaim_policy      = "Delete"
  volume_binding_mode = "WaitForFirstConsumer"
}


# The following resources are only applied when cluster_count=2 to set up vpc peering and the appropriate routes and
# security groups so traffic between VPCs is allowed. There is validation to ensure cluster_count can be 1 or 2.

# Each EKS cluster needs to allow ingress traffic from the other VPC.
resource "aws_security_group_rule" "allowingressfrom1-0" {
  count             = var.cluster_count > 1 ? 1 : 0
  type              = "ingress"
  from_port         = 0
  to_port           = 65535
  protocol          = "tcp"
  cidr_blocks       = [module.vpc[1].vpc_cidr_block]
  security_group_id = module.eks[0].cluster_primary_security_group_id
}

resource "aws_security_group_rule" "allowingressfrom0-1" {
  count             = var.cluster_count > 1 ? 1 : 0
  type              = "ingress"
  from_port         = 0
  to_port           = 65535
  protocol          = "tcp"
  cidr_blocks       = [module.vpc[0].vpc_cidr_block]
  security_group_id = module.eks[1].cluster_primary_security_group_id
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

# Add routes that so traffic going from VPC 0 to VPC 1 is routed through the peering connection.
resource "aws_route" "peering0" {
  # We have 2 route tables to add a route to, the public and private route tables.
  count                     = var.cluster_count > 1 ? 2 : 0
  route_table_id            = [module.vpc[0].public_route_table_ids[0], module.vpc[0].private_route_table_ids[0]][count.index]
  destination_cidr_block    = module.vpc[1].vpc_cidr_block
  vpc_peering_connection_id = aws_vpc_peering_connection.peer[0].id
}

# Add routes that so traffic going from VPC 1 to VPC 0 is routed through the peering connection.
resource "aws_route" "peering1" {
  # We have 2 route tables to add a route to, the public and private route tables.
  count                     = var.cluster_count > 1 ? 2 : 0
  route_table_id            = [module.vpc[1].public_route_table_ids[0], module.vpc[1].private_route_table_ids[0]][count.index]
  destination_cidr_block    = module.vpc[0].vpc_cidr_block
  vpc_peering_connection_id = aws_vpc_peering_connection.peer[0].id
}
