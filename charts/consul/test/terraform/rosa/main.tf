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

  provisioner "local-exec" {
    interpreter = ["/bin/bash", "-ec"]
    command     = <<-EOT
      rosa describe cluster -c "${self.triggers.cluster_name}" >/dev/null 2>&1 || \
      rosa create cluster \
        --cluster-name "${self.triggers.cluster_name}" \
        --region "${self.triggers.region}" \
        --version "${self.triggers.version}" \
        --sts \
        --mode auto \
        --yes \
        ${self.triggers.topology_flag} \
        --watch \
        --machine-cidr "${self.triggers.machine_cidr}" \
        --service-cidr "${self.triggers.service_cidr}" \
        --pod-cidr "${self.triggers.pod_cidr}" \
        --host-prefix ${self.triggers.host_prefix} \
        --subnet-ids "${self.triggers.subnet_ids}" \
        --replicas ${self.triggers.worker_replicas} \
        --compute-machine-type "${self.triggers.worker_type}" \
        ${self.triggers.additional_args}
    EOT
  }

  provisioner "local-exec" {
    when        = destroy
    interpreter = ["/bin/bash", "-ec"]
    command     = <<-EOT
      rosa describe cluster -c "${self.triggers.cluster_name}" >/dev/null 2>&1 && \
      rosa delete cluster -c "${self.triggers.cluster_name}" -y || true
    EOT
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

  provisioner "local-exec" {
    interpreter = ["/bin/bash", "-ec"]
    command     = <<-EOT
      rosa describe cluster -c "${self.triggers.cluster_name}" >/dev/null 2>&1 || \
      rosa create cluster \
        --cluster-name "${self.triggers.cluster_name}" \
        --region "${self.triggers.region}" \
        --version "${self.triggers.version}" \
        --sts \
        --mode auto \
        --yes \
        ${self.triggers.topology_flag} \
        --watch \
        --machine-cidr "${self.triggers.machine_cidr}" \
        --service-cidr "${self.triggers.service_cidr}" \
        --pod-cidr "${self.triggers.pod_cidr}" \
        --host-prefix ${self.triggers.host_prefix} \
        --subnet-ids "${self.triggers.subnet_ids}" \
        --replicas ${self.triggers.worker_replicas} \
        --compute-machine-type "${self.triggers.worker_type}" \
        ${self.triggers.additional_args}
    EOT
  }

  provisioner "local-exec" {
    when        = destroy
    interpreter = ["/bin/bash", "-ec"]
    command     = <<-EOT
      rosa describe cluster -c "${self.triggers.cluster_name}" >/dev/null 2>&1 && \
      rosa delete cluster -c "${self.triggers.cluster_name}" -y || true
    EOT
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

  provisioner "local-exec" {
    interpreter = ["/bin/bash", "-ec"]
    command     = <<-EOT
      find_node_sg() {
        local region="$1"
        local vpc_id="$2"
        local infra_id="$3"

        aws ec2 describe-security-groups \
          --region "$region" \
          --filters Name=vpc-id,Values="$vpc_id" Name=tag:red-hat-clustertype,Values=rosa \
          --query 'SecurityGroups[].{GroupId:GroupId,GroupName:GroupName,Tags:Tags}' \
          --output json | jq -r --arg infra_id "$infra_id" '
            .[]
            | select(
                (.GroupName == ($infra_id + "-node"))
                or (
                  ((.Tags // []) | any(.Key == "sigs.k8s.io/cluster-api-provider-aws/role" and .Value == "node"))
                  and ((.Tags // []) | any(.Key == ("sigs.k8s.io/cluster-api-provider-aws/cluster/" + $infra_id) and .Value == "owned"))
                )
              )
            | .GroupId
          ' | head -n 1
      }

      primary_infra_id="$(rosa describe cluster -c "${self.triggers.primary_cluster_name}" -o json | jq -r '.infra_id')"
      secondary_infra_id="$(rosa describe cluster -c "${self.triggers.secondary_cluster_name}" -o json | jq -r '.infra_id')"

      primary_worker_sg="$(find_node_sg "${self.triggers.primary_region}" "${self.triggers.primary_vpc_id}" "$${primary_infra_id}")"
      secondary_worker_sg="$(find_node_sg "${self.triggers.secondary_region}" "${self.triggers.secondary_vpc_id}" "$${secondary_infra_id}")"

      if [[ -z "$primary_worker_sg" || "$primary_worker_sg" == "None" ]]; then
        echo "Unable to find primary node security group for $primary_infra_id in ${self.triggers.primary_vpc_id}" >&2
        exit 1
      fi

      if [[ -z "$secondary_worker_sg" || "$secondary_worker_sg" == "None" ]]; then
        echo "Unable to find secondary node security group for $secondary_infra_id in ${self.triggers.secondary_vpc_id}" >&2
        exit 1
      fi

      aws ec2 authorize-security-group-ingress \
        --region "${self.triggers.primary_region}" \
        --group-id "$primary_worker_sg" \
        --ip-permissions '[
          {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"${self.triggers.secondary_vpc_cidr}","Description":"Consul RPC from secondary ROSA VPC"}]},
          {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${self.triggers.secondary_vpc_cidr}","Description":"Consul gossip TCP from secondary ROSA VPC"}]},
          {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${self.triggers.secondary_vpc_cidr}","Description":"Consul gossip UDP from secondary ROSA VPC"}]},
          {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"${self.triggers.secondary_vpc_cidr}","Description":"Consul HTTPS from secondary ROSA VPC"}]},
          {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"${self.triggers.secondary_vpc_cidr}","Description":"Consul gRPC TLS from secondary ROSA VPC"}]}
        ]' >/dev/null 2>&1 || true

      aws ec2 authorize-security-group-ingress \
        --region "${self.triggers.secondary_region}" \
        --group-id "$secondary_worker_sg" \
        --ip-permissions '[
          {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"${self.triggers.primary_vpc_cidr}","Description":"Consul RPC from primary ROSA VPC"}]},
          {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${self.triggers.primary_vpc_cidr}","Description":"Consul gossip TCP from primary ROSA VPC"}]},
          {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${self.triggers.primary_vpc_cidr}","Description":"Consul gossip UDP from primary ROSA VPC"}]},
          {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"${self.triggers.primary_vpc_cidr}","Description":"Consul HTTPS from primary ROSA VPC"}]},
          {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"${self.triggers.primary_vpc_cidr}","Description":"Consul gRPC TLS from primary ROSA VPC"}]}
        ]' >/dev/null 2>&1 || true
    EOT
  }

  depends_on = [
    null_resource.primary_cluster,
    null_resource.secondary_cluster,
  ]
}