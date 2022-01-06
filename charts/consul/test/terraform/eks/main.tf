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

data "aws_caller_identity" "caller" {}

resource "random_string" "suffix" {
  length  = 8
  special = false
}

module "vpc" {
  count   = var.cluster_count
  source  = "terraform-aws-modules/vpc/aws"
  version = "3.11.0"

  name                 = "consul-k8s-${random_id.suffix[count.index].dec}"
  cidr                 = format("10.%s.0.0/16", count.index)
  azs                  = data.aws_availability_zones.available.names
  private_subnets      = [format("10.%s.1.0/24", count.index), format("10.%s.2.0/24", count.index), format("10.%s.3.0/24", count.index)]
  public_subnets       = [format("10.%s.4.0/24", count.index), format("10.%s.5.0/24", count.index), format("10.%s.6.0/24", count.index)]
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
  cluster_version = "1.19"
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

resource "aws_security_group_rule" "allowingressfrom1-0" {
  type              = "ingress"
  from_port         = 0
  to_port           = 65535
  protocol          = "tcp"
  cidr_blocks       = [module.vpc[1].vpc_cidr_block]
  security_group_id = module.eks[0].cluster_primary_security_group_id
}

resource "aws_security_group_rule" "allowingressfrom0-1" {
  type              = "ingress"
  from_port         = 0
  to_port           = 65535
  protocol          = "tcp"
  cidr_blocks       = [module.vpc[0].vpc_cidr_block]
  security_group_id = module.eks[1].cluster_primary_security_group_id
}

# Requester's side of the connection.
resource "aws_vpc_peering_connection" "peer" {
  vpc_id        = module.vpc[0].vpc_id
  peer_vpc_id   = module.vpc[1].vpc_id
  peer_owner_id = data.aws_caller_identity.caller.account_id
  peer_region   = var.region
  auto_accept   = false

  tags = {
    Side = "Requester"
  }
}

# Accepter's side of the connection.
resource "aws_vpc_peering_connection_accepter" "peer" {
  vpc_peering_connection_id = aws_vpc_peering_connection.peer.id
  auto_accept               = true

  tags = {
    Side = "Accepter"
  }
}

# Get the route table ids that are associated with eks cluster 0
data "aws_route_tables" "rtseks0" {
  vpc_id = module.vpc[0].vpc_id

  filter {
    name   = "tag:Name"
    values = [format("%s-*", module.eks[0].cluster_id)]
  }
}
# Get the route table ids that are associated with eks cluster 1
data "aws_route_tables" "rtseks1" {
  vpc_id = module.vpc[1].vpc_id

  filter {
    name   = "tag:Name"
    values = [format("%s-*", module.eks[1].cluster_id)]
  }
}

resource "aws_route" "peering0" {
  count                     = 2 # we know its the public and private route tables
  route_table_id            = tolist(data.aws_route_tables.rtseks0.ids)[count.index]
  destination_cidr_block    = module.vpc[1].vpc_cidr_block
  vpc_peering_connection_id = aws_vpc_peering_connection.peer.id
}
resource "aws_route" "peering1" {
  count                     = 2
  route_table_id            = tolist(data.aws_route_tables.rtseks1.ids)[count.index]
  destination_cidr_block    = module.vpc[0].vpc_cidr_block
  vpc_peering_connection_id = aws_vpc_peering_connection.peer.id
}