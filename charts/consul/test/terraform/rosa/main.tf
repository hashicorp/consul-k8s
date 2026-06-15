# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

provider "aws" {
  alias  = "primary"
  region = var.primary_region

  dynamic "assume_role" {
    for_each = var.role_arn == "" ? [] : [var.role_arn]
    content {
      role_arn = assume_role.value
    }
  }
}

provider "aws" {
  alias  = "secondary"
  region = var.secondary_region

  dynamic "assume_role" {
    for_each = var.role_arn == "" ? [] : [var.role_arn]
    content {
      role_arn = assume_role.value
    }
  }
}

data "aws_availability_zones" "primary" {
  provider = aws.primary
  state    = "available"
}

data "aws_availability_zones" "secondary" {
  provider = aws.secondary
  state    = "available"
}

locals {
  primary_azs   = slice(data.aws_availability_zones.primary.names, 0, var.az_count)
  secondary_azs = slice(data.aws_availability_zones.secondary.names, 0, var.az_count)

  primary_private_subnet_cidrs   = [for index in range(var.az_count) : cidrsubnet(var.primary_vpc_cidr, 4, index)]
  primary_public_subnet_cidrs    = [for index in range(var.az_count) : cidrsubnet(var.primary_vpc_cidr, 4, index + 8)]
  secondary_private_subnet_cidrs = [for index in range(var.az_count) : cidrsubnet(var.secondary_vpc_cidr, 4, index)]
  secondary_public_subnet_cidrs  = [for index in range(var.az_count) : cidrsubnet(var.secondary_vpc_cidr, 4, index + 8)]

  primary_extra_args   = trimspace(join(" ", var.primary_additional_rosa_args))
  secondary_extra_args = trimspace(join(" ", var.secondary_additional_rosa_args))
  rosa_topology_flag   = var.az_count > 1 ? "--multi-az" : ""

  primary_subnet_ids_csv   = join(",", concat(aws_subnet.primary_private[*].id, aws_subnet.primary_public[*].id))
  secondary_subnet_ids_csv = join(",", concat(aws_subnet.secondary_private[*].id, aws_subnet.secondary_public[*].id))
}

resource "aws_vpc" "primary" {
  provider             = aws.primary
  cidr_block           = var.primary_vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(var.tags, {
    Name = "${var.primary_cluster_name}-vpc"
  })
}

resource "aws_vpc" "secondary" {
  provider             = aws.secondary
  cidr_block           = var.secondary_vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(var.tags, {
    Name = "${var.secondary_cluster_name}-vpc"
  })
}

resource "aws_internet_gateway" "primary" {
  provider = aws.primary
  vpc_id   = aws_vpc.primary.id

  tags = merge(var.tags, {
    Name = "${var.primary_cluster_name}-igw"
  })
}

resource "aws_internet_gateway" "secondary" {
  provider = aws.secondary
  vpc_id   = aws_vpc.secondary.id

  tags = merge(var.tags, {
    Name = "${var.secondary_cluster_name}-igw"
  })
}

resource "aws_subnet" "primary_public" {
  provider                = aws.primary
  count                   = var.az_count
  vpc_id                  = aws_vpc.primary.id
  cidr_block              = local.primary_public_subnet_cidrs[count.index]
  availability_zone       = local.primary_azs[count.index]
  map_public_ip_on_launch = true

  tags = merge(var.tags, {
    Name                     = "${var.primary_cluster_name}-public-${count.index}"
    "kubernetes.io/role/elb" = 1
  })
}

resource "aws_subnet" "primary_private" {
  provider          = aws.primary
  count             = var.az_count
  vpc_id            = aws_vpc.primary.id
  cidr_block        = local.primary_private_subnet_cidrs[count.index]
  availability_zone = local.primary_azs[count.index]

  tags = merge(var.tags, {
    Name                              = "${var.primary_cluster_name}-private-${count.index}"
    "kubernetes.io/role/internal-elb" = 1
  })
}

resource "aws_subnet" "secondary_public" {
  provider                = aws.secondary
  count                   = var.az_count
  vpc_id                  = aws_vpc.secondary.id
  cidr_block              = local.secondary_public_subnet_cidrs[count.index]
  availability_zone       = local.secondary_azs[count.index]
  map_public_ip_on_launch = true

  tags = merge(var.tags, {
    Name                     = "${var.secondary_cluster_name}-public-${count.index}"
    "kubernetes.io/role/elb" = 1
  })
}

resource "aws_subnet" "secondary_private" {
  provider          = aws.secondary
  count             = var.az_count
  vpc_id            = aws_vpc.secondary.id
  cidr_block        = local.secondary_private_subnet_cidrs[count.index]
  availability_zone = local.secondary_azs[count.index]

  tags = merge(var.tags, {
    Name                              = "${var.secondary_cluster_name}-private-${count.index}"
    "kubernetes.io/role/internal-elb" = 1
  })
}

resource "aws_eip" "primary_nat" {
  provider = aws.primary
  domain   = "vpc"

  tags = merge(var.tags, {
    Name = "${var.primary_cluster_name}-nat-eip"
  })
}

resource "aws_eip" "secondary_nat" {
  provider = aws.secondary
  domain   = "vpc"

  tags = merge(var.tags, {
    Name = "${var.secondary_cluster_name}-nat-eip"
  })
}

resource "aws_nat_gateway" "primary" {
  provider      = aws.primary
  allocation_id = aws_eip.primary_nat.id
  subnet_id     = aws_subnet.primary_public[0].id

  tags = merge(var.tags, {
    Name = "${var.primary_cluster_name}-nat"
  })

  depends_on = [aws_internet_gateway.primary]
}

resource "aws_nat_gateway" "secondary" {
  provider      = aws.secondary
  allocation_id = aws_eip.secondary_nat.id
  subnet_id     = aws_subnet.secondary_public[0].id

  tags = merge(var.tags, {
    Name = "${var.secondary_cluster_name}-nat"
  })

  depends_on = [aws_internet_gateway.secondary]
}

resource "aws_route_table" "primary_public" {
  provider = aws.primary
  vpc_id   = aws_vpc.primary.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.primary.id
  }

  route {
    cidr_block                = aws_vpc.secondary.cidr_block
    vpc_peering_connection_id = aws_vpc_peering_connection.peer.id
  }

  tags = merge(var.tags, {
    Name = "${var.primary_cluster_name}-public-rt"
  })
}

resource "aws_route_table" "secondary_public" {
  provider = aws.secondary
  vpc_id   = aws_vpc.secondary.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.secondary.id
  }

  route {
    cidr_block                = aws_vpc.primary.cidr_block
    vpc_peering_connection_id = aws_vpc_peering_connection.peer.id
  }

  tags = merge(var.tags, {
    Name = "${var.secondary_cluster_name}-public-rt"
  })
}

resource "aws_route_table" "primary_private" {
  provider = aws.primary
  vpc_id   = aws_vpc.primary.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.primary.id
  }

  route {
    cidr_block                = aws_vpc.secondary.cidr_block
    vpc_peering_connection_id = aws_vpc_peering_connection.peer.id
  }

  tags = merge(var.tags, {
    Name = "${var.primary_cluster_name}-private-rt"
  })
}

resource "aws_route_table" "secondary_private" {
  provider = aws.secondary
  vpc_id   = aws_vpc.secondary.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.secondary.id
  }

  route {
    cidr_block                = aws_vpc.primary.cidr_block
    vpc_peering_connection_id = aws_vpc_peering_connection.peer.id
  }

  tags = merge(var.tags, {
    Name = "${var.secondary_cluster_name}-private-rt"
  })
}

resource "aws_route_table_association" "primary_public" {
  provider       = aws.primary
  count          = var.az_count
  subnet_id      = aws_subnet.primary_public[count.index].id
  route_table_id = aws_route_table.primary_public.id
}

resource "aws_route_table_association" "primary_private" {
  provider       = aws.primary
  count          = var.az_count
  subnet_id      = aws_subnet.primary_private[count.index].id
  route_table_id = aws_route_table.primary_private.id
}

resource "aws_route_table_association" "secondary_public" {
  provider       = aws.secondary
  count          = var.az_count
  subnet_id      = aws_subnet.secondary_public[count.index].id
  route_table_id = aws_route_table.secondary_public.id
}

resource "aws_route_table_association" "secondary_private" {
  provider       = aws.secondary
  count          = var.az_count
  subnet_id      = aws_subnet.secondary_private[count.index].id
  route_table_id = aws_route_table.secondary_private.id
}

resource "aws_vpc_peering_connection" "peer" {
  provider    = aws.primary
  vpc_id      = aws_vpc.primary.id
  peer_vpc_id = aws_vpc.secondary.id
  peer_region = var.secondary_region
  auto_accept = false

  tags = merge(var.tags, {
    Name = "${var.primary_cluster_name}-${var.secondary_cluster_name}-pcx"
    Side = "requester"
  })
}

resource "aws_vpc_peering_connection_accepter" "peer" {
  provider                  = aws.secondary
  vpc_peering_connection_id = aws_vpc_peering_connection.peer.id
  auto_accept               = true

  tags = merge(var.tags, {
    Name = "${var.primary_cluster_name}-${var.secondary_cluster_name}-pcx"
    Side = "accepter"
  })
}

resource "aws_vpc_peering_connection_options" "requester" {
  provider                  = aws.primary
  vpc_peering_connection_id = aws_vpc_peering_connection.peer.id

  requester {
    allow_remote_vpc_dns_resolution = true
  }

  depends_on = [aws_vpc_peering_connection_accepter.peer]
}

resource "aws_vpc_peering_connection_options" "accepter" {
  provider                  = aws.secondary
  vpc_peering_connection_id = aws_vpc_peering_connection.peer.id

  accepter {
    allow_remote_vpc_dns_resolution = true
  }

  depends_on = [aws_vpc_peering_connection_accepter.peer]
}

resource "null_resource" "primary_cluster" {
  triggers = {
    cluster_name    = var.primary_cluster_name
    region          = var.primary_region
    version         = var.openshift_version
    topology_flag   = local.rosa_topology_flag
    machine_cidr    = var.primary_vpc_cidr
    service_cidr    = var.primary_service_cidr
    pod_cidr        = var.primary_pod_cidr
    host_prefix     = tostring(var.host_prefix)
    worker_replicas = tostring(var.worker_replicas)
    worker_type     = var.worker_instance_type
    subnet_ids      = local.primary_subnet_ids_csv
    additional_args = local.primary_extra_args
  }

  depends_on = [
    aws_vpc_peering_connection_options.requester,
    aws_vpc_peering_connection_options.accepter,
    aws_route_table.primary_public,
    aws_route_table.primary_private,
    aws_route_table.secondary_public,
    aws_route_table.secondary_private,
  ]
}

resource "null_resource" "secondary_cluster" {
  triggers = {
    cluster_name    = var.secondary_cluster_name
    region          = var.secondary_region
    version         = var.openshift_version
    topology_flag   = local.rosa_topology_flag
    machine_cidr    = var.secondary_vpc_cidr
    service_cidr    = var.secondary_service_cidr
    pod_cidr        = var.secondary_pod_cidr
    host_prefix     = tostring(var.host_prefix)
    worker_replicas = tostring(var.worker_replicas)
    worker_type     = var.worker_instance_type
    subnet_ids      = local.secondary_subnet_ids_csv
    additional_args = local.secondary_extra_args
  }

  depends_on = [
    aws_vpc_peering_connection_options.requester,
    aws_vpc_peering_connection_options.accepter,
    aws_route_table.primary_public,
    aws_route_table.primary_private,
    aws_route_table.secondary_public,
    aws_route_table.secondary_private,
  ]
}

resource "null_resource" "worker_sg_ingress" {
  triggers = {
    primary_cluster_name   = var.primary_cluster_name
    secondary_cluster_name = var.secondary_cluster_name
    primary_region         = var.primary_region
    secondary_region       = var.secondary_region
    primary_vpc_id         = aws_vpc.primary.id
    secondary_vpc_id       = aws_vpc.secondary.id
    primary_vpc_cidr       = var.primary_vpc_cidr
    secondary_vpc_cidr     = var.secondary_vpc_cidr
  }

  depends_on = [
    null_resource.primary_cluster,
    null_resource.secondary_cluster,
  ]
}