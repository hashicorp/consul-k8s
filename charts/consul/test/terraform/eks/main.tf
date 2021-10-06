provider "aws" {
  version = ">= 2.28.1"
  region  = var.region

  assume_role {
    role_arn         = var.role_arn
    duration_seconds = 2700
  }
}

resource "random_id" "suffix" {
  count       = var.cluster_count
  byte_length = 4
}

data "aws_availability_zones" "available" {}

resource "random_string" "suffix" {
  length  = 8
  special = false
}

module "vpc" {
  count   = var.cluster_count
  source  = "terraform-aws-modules/vpc/aws"
  version = "2.47.0"

  name                 = "consul-k8s-${random_id.suffix[count.index].dec}"
  cidr                 = "10.0.0.0/16"
  azs                  = data.aws_availability_zones.available.names
  private_subnets      = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
  public_subnets       = ["10.0.4.0/24", "10.0.5.0/24", "10.0.6.0/24"]
  enable_nat_gateway   = true
  single_nat_gateway   = true
  enable_dns_hostnames = true

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

  source  = "terraform-aws-modules/eks/aws"
  version = "17.20.0"

  cluster_name    = "consul-k8s-${random_id.suffix[count.index].dec}"
  cluster_version = "1.18"
  subnets         = module.vpc[count.index].private_subnets

  vpc_id = module.vpc[count.index].vpc_id

  node_groups = {
    first = {
      desired_capacity = 3
      max_capacity     = 3
      min_capacity     = 3

      instance_type = "m5.large"
    }
  }

  manage_aws_auth        = false
  write_kubeconfig       = true
  kubeconfig_output_path = pathexpand("~/.kube/consul-k8s-${random_id.suffix[count.index].dec}")

  tags = var.tags
}

data "aws_eks_cluster" "cluster" {
  count = var.cluster_count
  name  = module.eks[count.index].cluster_id
}

data "aws_eks_cluster_auth" "cluster" {
  count = var.cluster_count
  name  = module.eks[count.index].cluster_id
}