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
  for_each = local.cluster_pairs
  provider = aws.primary
  state    = "available"
}

data "aws_availability_zones" "secondary" {
  for_each = local.cluster_pairs
  provider = aws.secondary
  state    = "available"
}

locals {
  default_cluster_pairs = {
    default = {
      primary_cluster_name           = var.primary_cluster_name
      secondary_cluster_name         = var.secondary_cluster_name
      primary_vpc_cidr               = var.primary_vpc_cidr
      secondary_vpc_cidr             = var.secondary_vpc_cidr
      primary_service_cidr           = var.primary_service_cidr
      secondary_service_cidr         = var.secondary_service_cidr
      primary_pod_cidr               = var.primary_pod_cidr
      secondary_pod_cidr             = var.secondary_pod_cidr
      openshift_version              = var.openshift_version
      worker_instance_type           = var.worker_instance_type
      worker_replicas                = var.worker_replicas
      az_count                       = var.az_count
      host_prefix                    = var.host_prefix
      primary_additional_rosa_args   = var.primary_additional_rosa_args
      secondary_additional_rosa_args = var.secondary_additional_rosa_args
      tags                           = var.tags
    }
  }

  cluster_pairs_raw = length(var.cluster_pairs) > 0 ? var.cluster_pairs : local.default_cluster_pairs

  cluster_pairs = {
    for pair_key, pair in local.cluster_pairs_raw : pair_key => {
      primary_cluster_name           = pair.primary_cluster_name
      secondary_cluster_name         = pair.secondary_cluster_name
      primary_vpc_cidr               = pair.primary_vpc_cidr
      secondary_vpc_cidr             = pair.secondary_vpc_cidr
      primary_service_cidr           = pair.primary_service_cidr
      secondary_service_cidr         = pair.secondary_service_cidr
      primary_pod_cidr               = pair.primary_pod_cidr
      secondary_pod_cidr             = pair.secondary_pod_cidr
      openshift_version              = try(pair.openshift_version, var.openshift_version)
      worker_instance_type           = try(pair.worker_instance_type, var.worker_instance_type)
      worker_replicas                = try(pair.worker_replicas, var.worker_replicas)
      az_count                       = try(pair.az_count, var.az_count)
      host_prefix                    = try(pair.host_prefix, var.host_prefix)
      primary_additional_rosa_args   = try(pair.primary_additional_rosa_args, var.primary_additional_rosa_args)
      secondary_additional_rosa_args = try(pair.secondary_additional_rosa_args, var.secondary_additional_rosa_args)
      tags                           = merge(var.tags, try(pair.tags, {}))
    }
  }

  pair_settings = {
    for pair_key, pair in local.cluster_pairs : pair_key => {
      primary_azs                    = slice(data.aws_availability_zones.primary[pair_key].names, 0, pair.az_count)
      secondary_azs                  = slice(data.aws_availability_zones.secondary[pair_key].names, 0, pair.az_count)
      primary_private_subnet_cidrs   = [for index in range(pair.az_count) : cidrsubnet(pair.primary_vpc_cidr, 4, index)]
      primary_public_subnet_cidrs    = [for index in range(pair.az_count) : cidrsubnet(pair.primary_vpc_cidr, 4, index + 8)]
      secondary_private_subnet_cidrs = [for index in range(pair.az_count) : cidrsubnet(pair.secondary_vpc_cidr, 4, index)]
      secondary_public_subnet_cidrs  = [for index in range(pair.az_count) : cidrsubnet(pair.secondary_vpc_cidr, 4, index + 8)]
      primary_extra_args             = trimspace(join(" ", pair.primary_additional_rosa_args))
      secondary_extra_args           = trimspace(join(" ", pair.secondary_additional_rosa_args))
      rosa_topology_flag             = pair.az_count > 1 ? "--multi-az" : ""
    }
  }

  primary_public_subnets = merge([
    for pair_key, pair in local.cluster_pairs : {
      for index, cidr in local.pair_settings[pair_key].primary_public_subnet_cidrs : "${pair_key}-public-${index}" => {
        pair_key = pair_key
        index    = index
        cidr     = cidr
        az       = local.pair_settings[pair_key].primary_azs[index]
      }
    }
  ]...)

  primary_private_subnets = merge([
    for pair_key, pair in local.cluster_pairs : {
      for index, cidr in local.pair_settings[pair_key].primary_private_subnet_cidrs : "${pair_key}-private-${index}" => {
        pair_key = pair_key
        index    = index
        cidr     = cidr
        az       = local.pair_settings[pair_key].primary_azs[index]
      }
    }
  ]...)

  secondary_public_subnets = merge([
    for pair_key, pair in local.cluster_pairs : {
      for index, cidr in local.pair_settings[pair_key].secondary_public_subnet_cidrs : "${pair_key}-public-${index}" => {
        pair_key = pair_key
        index    = index
        cidr     = cidr
        az       = local.pair_settings[pair_key].secondary_azs[index]
      }
    }
  ]...)

  secondary_private_subnets = merge([
    for pair_key, pair in local.cluster_pairs : {
      for index, cidr in local.pair_settings[pair_key].secondary_private_subnet_cidrs : "${pair_key}-private-${index}" => {
        pair_key = pair_key
        index    = index
        cidr     = cidr
        az       = local.pair_settings[pair_key].secondary_azs[index]
      }
    }
  ]...)

  primary_private_subnet_ids = {
    for pair_key in keys(local.cluster_pairs) : pair_key => [
      for subnet_key in sort([for key, subnet in local.primary_private_subnets : key if subnet.pair_key == pair_key]) : aws_subnet.primary_private[subnet_key].id
    ]
  }

  primary_public_subnet_ids = {
    for pair_key in keys(local.cluster_pairs) : pair_key => [
      for subnet_key in sort([for key, subnet in local.primary_public_subnets : key if subnet.pair_key == pair_key]) : aws_subnet.primary_public[subnet_key].id
    ]
  }

  secondary_private_subnet_ids = {
    for pair_key in keys(local.cluster_pairs) : pair_key => [
      for subnet_key in sort([for key, subnet in local.secondary_private_subnets : key if subnet.pair_key == pair_key]) : aws_subnet.secondary_private[subnet_key].id
    ]
  }

  secondary_public_subnet_ids = {
    for pair_key in keys(local.cluster_pairs) : pair_key => [
      for subnet_key in sort([for key, subnet in local.secondary_public_subnets : key if subnet.pair_key == pair_key]) : aws_subnet.secondary_public[subnet_key].id
    ]
  }

  primary_subnet_ids_csv = {
    for pair_key in keys(local.cluster_pairs) : pair_key => join(",", concat(local.primary_private_subnet_ids[pair_key], local.primary_public_subnet_ids[pair_key]))
  }

  secondary_subnet_ids_csv = {
    for pair_key in keys(local.cluster_pairs) : pair_key => join(",", concat(local.secondary_private_subnet_ids[pair_key], local.secondary_public_subnet_ids[pair_key]))
  }

  pair_keys       = sort(keys(local.cluster_pairs))
  single_pair_key = length(local.pair_keys) == 1 ? local.pair_keys[0] : null

  primary_rosa_create_commands = {
    for pair_key, pair in local.cluster_pairs : pair_key => join(" \\\n+  ", compact([
      "rosa create cluster",
      "--cluster-name ${pair.primary_cluster_name}",
      "--region ${var.primary_region}",
      "--version ${pair.openshift_version}",
      "--sts",
      "--mode auto",
      "--yes",
      local.pair_settings[pair_key].rosa_topology_flag,
      "--watch",
      "--machine-cidr ${pair.primary_vpc_cidr}",
      "--service-cidr ${pair.primary_service_cidr}",
      "--pod-cidr ${pair.primary_pod_cidr}",
      "--host-prefix ${pair.host_prefix}",
      "--subnet-ids ${local.primary_subnet_ids_csv[pair_key]}",
      "--replicas ${pair.worker_replicas}",
      "--compute-machine-type ${pair.worker_instance_type}",
      local.pair_settings[pair_key].primary_extra_args,
    ]))
  }

  secondary_rosa_create_commands = {
    for pair_key, pair in local.cluster_pairs : pair_key => join(" \\\n+  ", compact([
      "rosa create cluster",
      "--cluster-name ${pair.secondary_cluster_name}",
      "--region ${var.secondary_region}",
      "--version ${pair.openshift_version}",
      "--sts",
      "--mode auto",
      "--yes",
      local.pair_settings[pair_key].rosa_topology_flag,
      "--watch",
      "--machine-cidr ${pair.secondary_vpc_cidr}",
      "--service-cidr ${pair.secondary_service_cidr}",
      "--pod-cidr ${pair.secondary_pod_cidr}",
      "--host-prefix ${pair.host_prefix}",
      "--subnet-ids ${local.secondary_subnet_ids_csv[pair_key]}",
      "--replicas ${pair.worker_replicas}",
      "--compute-machine-type ${pair.worker_instance_type}",
      local.pair_settings[pair_key].secondary_extra_args,
    ]))
  }

  worker_security_group_ingress_commands = {
    for pair_key, pair in local.cluster_pairs : pair_key => trimspace(<<EOT
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

primary_infra_id="$(rosa describe cluster -c "${pair.primary_cluster_name}" -o json | jq -r '.infra_id')"
secondary_infra_id="$(rosa describe cluster -c "${pair.secondary_cluster_name}" -o json | jq -r '.infra_id')"

primary_worker_sg="$(find_node_sg "${var.primary_region}" "${aws_vpc.primary[pair_key].id}" "$primary_infra_id")"
secondary_worker_sg="$(find_node_sg "${var.secondary_region}" "${aws_vpc.secondary[pair_key].id}" "$secondary_infra_id")"

aws ec2 authorize-security-group-ingress \
  --region "${var.primary_region}" \
  --group-id "$primary_worker_sg" \
  --ip-permissions '[
    {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"${pair.secondary_vpc_cidr}","Description":"Consul RPC from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${pair.secondary_vpc_cidr}","Description":"Consul gossip TCP from secondary ROSA VPC"}]},
    {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${pair.secondary_vpc_cidr}","Description":"Consul gossip UDP from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"${pair.secondary_vpc_cidr}","Description":"Consul HTTPS from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"${pair.secondary_vpc_cidr}","Description":"Consul gRPC TLS from secondary ROSA VPC"}]}
  ]' >/dev/null 2>&1 || true

aws ec2 authorize-security-group-ingress \
  --region "${var.secondary_region}" \
  --group-id "$secondary_worker_sg" \
  --ip-permissions '[
    {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"${pair.primary_vpc_cidr}","Description":"Consul RPC from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${pair.primary_vpc_cidr}","Description":"Consul gossip TCP from primary ROSA VPC"}]},
    {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${pair.primary_vpc_cidr}","Description":"Consul gossip UDP from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"${pair.primary_vpc_cidr}","Description":"Consul HTTPS from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"${pair.primary_vpc_cidr}","Description":"Consul gRPC TLS from primary ROSA VPC"}]}
  ]' >/dev/null 2>&1 || true
EOT
    )
  }

  pair_bootstrap_scripts = {
    for pair_key, pair in local.cluster_pairs : pair_key => trimspace(<<EOT
#!/usr/bin/env bash
set -euo pipefail

PRIMARY_CLUSTER_NAME="${pair.primary_cluster_name}"
SECONDARY_CLUSTER_NAME="${pair.secondary_cluster_name}"

create_or_wait_cluster() {
  local cluster_name="$1"
  shift

  if rosa describe cluster -c "$cluster_name" >/dev/null 2>&1; then
    echo "Cluster $cluster_name already exists; waiting for ready state"
  else
    echo "Creating cluster $cluster_name"
    rosa create cluster "$@"
  fi
}

wait_for_cluster_ready() {
  local cluster_name="$1"
  local state

  while true; do
    state="$(rosa describe cluster -c "$cluster_name" -o json | jq -r '.state // empty')"

    if [[ "$state" == "ready" ]]; then
      echo "Cluster $cluster_name is ready"
      return 0
    fi

    if [[ -n "$state" ]]; then
      echo "Cluster $cluster_name state: $state"
    else
      echo "Cluster $cluster_name is not yet visible"
    fi

    sleep 30
  done
}

create_or_wait_cluster "$PRIMARY_CLUSTER_NAME" \
  --cluster-name "$PRIMARY_CLUSTER_NAME" \
  --region "${var.primary_region}" \
  --version "${pair.openshift_version}" \
  --sts \
  --mode auto \
  --yes \
  ${local.pair_settings[pair_key].rosa_topology_flag} \
  --watch \
  --machine-cidr "${pair.primary_vpc_cidr}" \
  --service-cidr "${pair.primary_service_cidr}" \
  --pod-cidr "${pair.primary_pod_cidr}" \
  --host-prefix ${pair.host_prefix} \
  --subnet-ids "${local.primary_subnet_ids_csv[pair_key]}" \
  --replicas ${pair.worker_replicas} \
  --compute-machine-type "${pair.worker_instance_type}" \
  ${local.pair_settings[pair_key].primary_extra_args} &
primary_cluster_pid=$!

create_or_wait_cluster "$SECONDARY_CLUSTER_NAME" \
  --cluster-name "$SECONDARY_CLUSTER_NAME" \
  --region "${var.secondary_region}" \
  --version "${pair.openshift_version}" \
  --sts \
  --mode auto \
  --yes \
  ${local.pair_settings[pair_key].rosa_topology_flag} \
  --watch \
  --machine-cidr "${pair.secondary_vpc_cidr}" \
  --service-cidr "${pair.secondary_service_cidr}" \
  --pod-cidr "${pair.secondary_pod_cidr}" \
  --host-prefix ${pair.host_prefix} \
  --subnet-ids "${local.secondary_subnet_ids_csv[pair_key]}" \
  --replicas ${pair.worker_replicas} \
  --compute-machine-type "${pair.worker_instance_type}" \
  ${local.pair_settings[pair_key].secondary_extra_args} &
secondary_cluster_pid=$!

wait "$primary_cluster_pid"
wait "$secondary_cluster_pid"

${local.worker_security_group_ingress_commands[pair_key]}

echo "ROSA cluster pair ${pair_key} is ready"
EOT
    )
  }
}

resource "aws_vpc" "primary" {
  for_each             = local.cluster_pairs
  provider             = aws.primary
  cidr_block           = each.value.primary_vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(each.value.tags, {
    Name = "${each.value.primary_cluster_name}-vpc"
  })
}

resource "aws_vpc" "secondary" {
  for_each             = local.cluster_pairs
  provider             = aws.secondary
  cidr_block           = each.value.secondary_vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(each.value.tags, {
    Name = "${each.value.secondary_cluster_name}-vpc"
  })
}

resource "aws_internet_gateway" "primary" {
  for_each = local.cluster_pairs
  provider = aws.primary
  vpc_id   = aws_vpc.primary[each.key].id

  tags = merge(each.value.tags, {
    Name = "${each.value.primary_cluster_name}-igw"
  })
}

resource "aws_internet_gateway" "secondary" {
  for_each = local.cluster_pairs
  provider = aws.secondary
  vpc_id   = aws_vpc.secondary[each.key].id

  tags = merge(each.value.tags, {
    Name = "${each.value.secondary_cluster_name}-igw"
  })
}

resource "aws_subnet" "primary_public" {
  for_each                = local.primary_public_subnets
  provider                = aws.primary
  vpc_id                  = aws_vpc.primary[each.value.pair_key].id
  cidr_block              = each.value.cidr
  availability_zone       = each.value.az
  map_public_ip_on_launch = true

  tags = merge(local.cluster_pairs[each.value.pair_key].tags, {
    Name                     = "${local.cluster_pairs[each.value.pair_key].primary_cluster_name}-public-${each.value.index}"
    "kubernetes.io/role/elb" = 1
  })
}

resource "aws_subnet" "primary_private" {
  for_each          = local.primary_private_subnets
  provider          = aws.primary
  vpc_id            = aws_vpc.primary[each.value.pair_key].id
  cidr_block        = each.value.cidr
  availability_zone = each.value.az

  tags = merge(local.cluster_pairs[each.value.pair_key].tags, {
    Name                              = "${local.cluster_pairs[each.value.pair_key].primary_cluster_name}-private-${each.value.index}"
    "kubernetes.io/role/internal-elb" = 1
  })
}

resource "aws_subnet" "secondary_public" {
  for_each                = local.secondary_public_subnets
  provider                = aws.secondary
  vpc_id                  = aws_vpc.secondary[each.value.pair_key].id
  cidr_block              = each.value.cidr
  availability_zone       = each.value.az
  map_public_ip_on_launch = true

  tags = merge(local.cluster_pairs[each.value.pair_key].tags, {
    Name                     = "${local.cluster_pairs[each.value.pair_key].secondary_cluster_name}-public-${each.value.index}"
    "kubernetes.io/role/elb" = 1
  })
}

resource "aws_subnet" "secondary_private" {
  for_each          = local.secondary_private_subnets
  provider          = aws.secondary
  vpc_id            = aws_vpc.secondary[each.value.pair_key].id
  cidr_block        = each.value.cidr
  availability_zone = each.value.az

  tags = merge(local.cluster_pairs[each.value.pair_key].tags, {
    Name                              = "${local.cluster_pairs[each.value.pair_key].secondary_cluster_name}-private-${each.value.index}"
    "kubernetes.io/role/internal-elb" = 1
  })
}

resource "aws_eip" "primary_nat" {
  for_each = local.cluster_pairs
  provider = aws.primary
  domain   = "vpc"

  tags = merge(each.value.tags, {
    Name = "${each.value.primary_cluster_name}-nat-eip"
  })
}

resource "aws_eip" "secondary_nat" {
  for_each = local.cluster_pairs
  provider = aws.secondary
  domain   = "vpc"

  tags = merge(each.value.tags, {
    Name = "${each.value.secondary_cluster_name}-nat-eip"
  })
}

resource "aws_nat_gateway" "primary" {
  for_each      = local.cluster_pairs
  provider      = aws.primary
  allocation_id = aws_eip.primary_nat[each.key].id
  subnet_id     = aws_subnet.primary_public["${each.key}-public-0"].id

  tags = merge(each.value.tags, {
    Name = "${each.value.primary_cluster_name}-nat"
  })

  depends_on = [aws_internet_gateway.primary]
}

resource "aws_nat_gateway" "secondary" {
  for_each      = local.cluster_pairs
  provider      = aws.secondary
  allocation_id = aws_eip.secondary_nat[each.key].id
  subnet_id     = aws_subnet.secondary_public["${each.key}-public-0"].id

  tags = merge(each.value.tags, {
    Name = "${each.value.secondary_cluster_name}-nat"
  })

  depends_on = [aws_internet_gateway.secondary]
}

resource "aws_route_table" "primary_public" {
  for_each = local.cluster_pairs
  provider = aws.primary
  vpc_id   = aws_vpc.primary[each.key].id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.primary[each.key].id
  }

  route {
    cidr_block                = aws_vpc.secondary[each.key].cidr_block
    vpc_peering_connection_id = aws_vpc_peering_connection.peer[each.key].id
  }

  tags = merge(each.value.tags, {
    Name = "${each.value.primary_cluster_name}-public-rt"
  })
}

resource "aws_route_table" "secondary_public" {
  for_each = local.cluster_pairs
  provider = aws.secondary
  vpc_id   = aws_vpc.secondary[each.key].id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.secondary[each.key].id
  }

  route {
    cidr_block                = aws_vpc.primary[each.key].cidr_block
    vpc_peering_connection_id = aws_vpc_peering_connection.peer[each.key].id
  }

  tags = merge(each.value.tags, {
    Name = "${each.value.secondary_cluster_name}-public-rt"
  })
}

resource "aws_route_table" "primary_private" {
  for_each = local.cluster_pairs
  provider = aws.primary
  vpc_id   = aws_vpc.primary[each.key].id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.primary[each.key].id
  }

  route {
    cidr_block                = aws_vpc.secondary[each.key].cidr_block
    vpc_peering_connection_id = aws_vpc_peering_connection.peer[each.key].id
  }

  tags = merge(each.value.tags, {
    Name = "${each.value.primary_cluster_name}-private-rt"
  })
}

resource "aws_route_table" "secondary_private" {
  for_each = local.cluster_pairs
  provider = aws.secondary
  vpc_id   = aws_vpc.secondary[each.key].id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.secondary[each.key].id
  }

  route {
    cidr_block                = aws_vpc.primary[each.key].cidr_block
    vpc_peering_connection_id = aws_vpc_peering_connection.peer[each.key].id
  }

  tags = merge(each.value.tags, {
    Name = "${each.value.secondary_cluster_name}-private-rt"
  })
}

resource "aws_route_table_association" "primary_public" {
  for_each       = local.primary_public_subnets
  provider       = aws.primary
  subnet_id      = aws_subnet.primary_public[each.key].id
  route_table_id = aws_route_table.primary_public[each.value.pair_key].id
}

resource "aws_route_table_association" "primary_private" {
  for_each       = local.primary_private_subnets
  provider       = aws.primary
  subnet_id      = aws_subnet.primary_private[each.key].id
  route_table_id = aws_route_table.primary_private[each.value.pair_key].id
}

resource "aws_route_table_association" "secondary_public" {
  for_each       = local.secondary_public_subnets
  provider       = aws.secondary
  subnet_id      = aws_subnet.secondary_public[each.key].id
  route_table_id = aws_route_table.secondary_public[each.value.pair_key].id
}

resource "aws_route_table_association" "secondary_private" {
  for_each       = local.secondary_private_subnets
  provider       = aws.secondary
  subnet_id      = aws_subnet.secondary_private[each.key].id
  route_table_id = aws_route_table.secondary_private[each.value.pair_key].id
}

resource "aws_vpc_peering_connection" "peer" {
  for_each    = local.cluster_pairs
  provider    = aws.primary
  vpc_id      = aws_vpc.primary[each.key].id
  peer_vpc_id = aws_vpc.secondary[each.key].id
  peer_region = var.secondary_region
  auto_accept = false

  tags = merge(each.value.tags, {
    Name = "${each.value.primary_cluster_name}-${each.value.secondary_cluster_name}-pcx"
    Side = "requester"
  })
}

resource "aws_vpc_peering_connection_accepter" "peer" {
  for_each                  = local.cluster_pairs
  provider                  = aws.secondary
  vpc_peering_connection_id = aws_vpc_peering_connection.peer[each.key].id
  auto_accept               = true

  tags = merge(each.value.tags, {
    Name = "${each.value.primary_cluster_name}-${each.value.secondary_cluster_name}-pcx"
    Side = "accepter"
  })
}

resource "aws_vpc_peering_connection_options" "requester" {
  for_each                  = local.cluster_pairs
  provider                  = aws.primary
  vpc_peering_connection_id = aws_vpc_peering_connection.peer[each.key].id

  requester {
    allow_remote_vpc_dns_resolution = true
  }

  depends_on = [aws_vpc_peering_connection_accepter.peer]
}

resource "aws_vpc_peering_connection_options" "accepter" {
  for_each                  = local.cluster_pairs
  provider                  = aws.secondary
  vpc_peering_connection_id = aws_vpc_peering_connection.peer[each.key].id

  accepter {
    allow_remote_vpc_dns_resolution = true
  }

  depends_on = [aws_vpc_peering_connection_accepter.peer]
}

resource "null_resource" "primary_cluster" {
  for_each = local.cluster_pairs

  triggers = {
    cluster_name    = each.value.primary_cluster_name
    region          = var.primary_region
    version         = each.value.openshift_version
    topology_flag   = local.pair_settings[each.key].rosa_topology_flag
    machine_cidr    = each.value.primary_vpc_cidr
    service_cidr    = each.value.primary_service_cidr
    pod_cidr        = each.value.primary_pod_cidr
    host_prefix     = tostring(each.value.host_prefix)
    worker_replicas = tostring(each.value.worker_replicas)
    worker_type     = each.value.worker_instance_type
    subnet_ids      = local.primary_subnet_ids_csv[each.key]
    additional_args = local.pair_settings[each.key].primary_extra_args
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
  for_each = local.cluster_pairs

  triggers = {
    cluster_name    = each.value.secondary_cluster_name
    region          = var.secondary_region
    version         = each.value.openshift_version
    topology_flag   = local.pair_settings[each.key].rosa_topology_flag
    machine_cidr    = each.value.secondary_vpc_cidr
    service_cidr    = each.value.secondary_service_cidr
    pod_cidr        = each.value.secondary_pod_cidr
    host_prefix     = tostring(each.value.host_prefix)
    worker_replicas = tostring(each.value.worker_replicas)
    worker_type     = each.value.worker_instance_type
    subnet_ids      = local.secondary_subnet_ids_csv[each.key]
    additional_args = local.pair_settings[each.key].secondary_extra_args
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
  for_each = local.cluster_pairs

  triggers = {
    primary_cluster_name   = each.value.primary_cluster_name
    secondary_cluster_name = each.value.secondary_cluster_name
    primary_region         = var.primary_region
    secondary_region       = var.secondary_region
    primary_vpc_id         = aws_vpc.primary[each.key].id
    secondary_vpc_id       = aws_vpc.secondary[each.key].id
    primary_vpc_cidr       = each.value.primary_vpc_cidr
    secondary_vpc_cidr     = each.value.secondary_vpc_cidr
  }

  depends_on = [
    null_resource.primary_cluster,
    null_resource.secondary_cluster,
  ]
}