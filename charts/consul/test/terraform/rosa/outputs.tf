# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

output "primary_cluster_name" {
  value = var.primary_cluster_name
}

output "secondary_cluster_name" {
  value = var.secondary_cluster_name
}

output "primary_vpc_id" {
  value = aws_vpc.primary.id
}

output "secondary_vpc_id" {
  value = aws_vpc.secondary.id
}

output "vpc_peering_connection_id" {
  value = aws_vpc_peering_connection.peer.id
}

output "primary_private_subnet_ids" {
  value = aws_subnet.primary_private[*].id
}

output "secondary_private_subnet_ids" {
  value = aws_subnet.secondary_private[*].id
}

output "primary_rosa_create_command" {
  value = join(" \\\n+  ", compact([
    "rosa create cluster",
    "--cluster-name ${var.primary_cluster_name}",
    "--region ${var.primary_region}",
    "--version ${var.openshift_version}",
    "--sts",
    "--mode auto",
    "--yes",
    local.rosa_topology_flag,
    "--watch",
    "--machine-cidr ${var.primary_vpc_cidr}",
    "--service-cidr ${var.primary_service_cidr}",
    "--pod-cidr ${var.primary_pod_cidr}",
    "--host-prefix ${var.host_prefix}",
    "--subnet-ids ${local.primary_subnet_ids_csv}",
    "--replicas ${var.worker_replicas}",
    "--compute-machine-type ${var.worker_instance_type}",
    local.primary_extra_args,
  ]))
}

output "secondary_rosa_create_command" {
  value = join(" \\\n+  ", compact([
    "rosa create cluster",
    "--cluster-name ${var.secondary_cluster_name}",
    "--region ${var.secondary_region}",
    "--version ${var.openshift_version}",
    "--sts",
    "--mode auto",
    "--yes",
    local.rosa_topology_flag,
    "--watch",
    "--machine-cidr ${var.secondary_vpc_cidr}",
    "--service-cidr ${var.secondary_service_cidr}",
    "--pod-cidr ${var.secondary_pod_cidr}",
    "--host-prefix ${var.host_prefix}",
    "--subnet-ids ${local.secondary_subnet_ids_csv}",
    "--replicas ${var.worker_replicas}",
    "--compute-machine-type ${var.worker_instance_type}",
    local.secondary_extra_args,
  ]))
}

output "worker_security_group_ingress_command" {
  value = trimspace(<<EOT
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

    primary_infra_id="$(rosa describe cluster -c "${var.primary_cluster_name}" -o json | jq -r '.infra_id')"
    secondary_infra_id="$(rosa describe cluster -c "${var.secondary_cluster_name}" -o json | jq -r '.infra_id')"

    primary_worker_sg="$(find_node_sg "${var.primary_region}" "${aws_vpc.primary.id}" "$primary_infra_id")"
    secondary_worker_sg="$(find_node_sg "${var.secondary_region}" "${aws_vpc.secondary.id}" "$secondary_infra_id")"

    aws ec2 authorize-security-group-ingress \
      --region "${var.primary_region}" \
      --group-id "$primary_worker_sg" \
      --ip-permissions '[
        {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul RPC from secondary ROSA VPC"}]},
        {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul gossip TCP from secondary ROSA VPC"}]},
        {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul gossip UDP from secondary ROSA VPC"}]},
        {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul HTTPS from secondary ROSA VPC"}]},
        {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul gRPC TLS from secondary ROSA VPC"}]}
      ]' >/dev/null 2>&1 || true

    aws ec2 authorize-security-group-ingress \
      --region "${var.secondary_region}" \
      --group-id "$secondary_worker_sg" \
      --ip-permissions '[
        {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul RPC from primary ROSA VPC"}]},
        {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul gossip TCP from primary ROSA VPC"}]},
        {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul gossip UDP from primary ROSA VPC"}]},
        {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul HTTPS from primary ROSA VPC"}]},
        {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul gRPC TLS from primary ROSA VPC"}]}
      ]' >/dev/null 2>&1 || true
EOT
  )
}

output "worker_security_group_note" {
  value = "Run worker_security_group_ingress_command after both ROSA clusters become ready to allow bidirectional worker security group ingress for 8300, 8301/tcp, 8301/udp, 8501, and 8502."
}

output "rosa_cluster_bootstrap_script" {
  value = trimspace(<<EOT
#!/usr/bin/env bash
set -euo pipefail

PRIMARY_CLUSTER_NAME="${var.primary_cluster_name}"
SECONDARY_CLUSTER_NAME="${var.secondary_cluster_name}"
PRIMARY_REGION="${var.primary_region}"
SECONDARY_REGION="${var.secondary_region}"
PRIMARY_VPC_ID="${aws_vpc.primary.id}"
SECONDARY_VPC_ID="${aws_vpc.secondary.id}"
PRIMARY_VPC_CIDR="${var.primary_vpc_cidr}"
SECONDARY_VPC_CIDR="${var.secondary_vpc_cidr}"

create_or_wait_cluster() {
  local cluster_name="$1"
  shift

  if rosa describe cluster -c "$cluster_name" >/dev/null 2>&1; then
    echo "Cluster $cluster_name already exists; waiting for ready state"
  else
    echo "Creating cluster $cluster_name"
    rosa create cluster "$@"
  fi

  wait_for_cluster_ready "$cluster_name"
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

create_or_wait_cluster "$PRIMARY_CLUSTER_NAME" \
  --cluster-name "$PRIMARY_CLUSTER_NAME" \
  --region "$PRIMARY_REGION" \
  --version "${var.openshift_version}" \
  --sts \
  --mode auto \
  --yes \
  ${local.rosa_topology_flag} \
  --watch \
  --machine-cidr "${var.primary_vpc_cidr}" \
  --service-cidr "${var.primary_service_cidr}" \
  --pod-cidr "${var.primary_pod_cidr}" \
  --host-prefix ${var.host_prefix} \
  --subnet-ids "${local.primary_subnet_ids_csv}" \
  --replicas ${var.worker_replicas} \
  --compute-machine-type "${var.worker_instance_type}" \
  ${local.primary_extra_args} &
primary_cluster_pid=$!

create_or_wait_cluster "$SECONDARY_CLUSTER_NAME" \
  --cluster-name "$SECONDARY_CLUSTER_NAME" \
  --region "$SECONDARY_REGION" \
  --version "${var.openshift_version}" \
  --sts \
  --mode auto \
  --yes \
  ${local.rosa_topology_flag} \
  --watch \
  --machine-cidr "${var.secondary_vpc_cidr}" \
  --service-cidr "${var.secondary_service_cidr}" \
  --pod-cidr "${var.secondary_pod_cidr}" \
  --host-prefix ${var.host_prefix} \
  --subnet-ids "${local.secondary_subnet_ids_csv}" \
  --replicas ${var.worker_replicas} \
  --compute-machine-type "${var.worker_instance_type}" \
  ${local.secondary_extra_args} &
secondary_cluster_pid=$!

wait "$primary_cluster_pid"
wait "$secondary_cluster_pid"

primary_infra_id="$(rosa describe cluster -c "$PRIMARY_CLUSTER_NAME" -o json | jq -r '.infra_id')"
secondary_infra_id="$(rosa describe cluster -c "$SECONDARY_CLUSTER_NAME" -o json | jq -r '.infra_id')"

primary_worker_sg="$(find_node_sg "$PRIMARY_REGION" "$PRIMARY_VPC_ID" "$primary_infra_id")"
secondary_worker_sg="$(find_node_sg "$SECONDARY_REGION" "$SECONDARY_VPC_ID" "$secondary_infra_id")"

if [[ -z "$primary_worker_sg" || "$primary_worker_sg" == "None" ]]; then
  echo "Unable to find primary node security group for $primary_infra_id in $PRIMARY_VPC_ID" >&2
  exit 1
fi

if [[ -z "$secondary_worker_sg" || "$secondary_worker_sg" == "None" ]]; then
  echo "Unable to find secondary node security group for $secondary_infra_id in $SECONDARY_VPC_ID" >&2
  exit 1
fi

aws ec2 authorize-security-group-ingress \
  --region "$PRIMARY_REGION" \
  --group-id "$primary_worker_sg" \
  --ip-permissions '[
    {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul RPC from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul gossip TCP from secondary ROSA VPC"}]},
    {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul gossip UDP from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul HTTPS from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"${var.secondary_vpc_cidr}","Description":"Consul gRPC TLS from secondary ROSA VPC"}]}
  ]' >/dev/null 2>&1 || true

aws ec2 authorize-security-group-ingress \
  --region "$SECONDARY_REGION" \
  --group-id "$secondary_worker_sg" \
  --ip-permissions '[
    {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul RPC from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul gossip TCP from primary ROSA VPC"}]},
    {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul gossip UDP from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul HTTPS from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"${var.primary_vpc_cidr}","Description":"Consul gRPC TLS from primary ROSA VPC"}]}
  ]' >/dev/null 2>&1 || true

echo "ROSA clusters are ready and worker security group ingress has been applied"
EOT
  )
}