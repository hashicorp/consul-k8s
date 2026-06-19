#!/usr/bin/env bash
set -euo pipefail

PRIMARY_CLUSTER_NAME="test-bed-419-east"
SECONDARY_CLUSTER_NAME="test-bed-419-west"

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
  --region "us-east-2" \
  --version "4.19.27" \
  --sts \
  --mode auto \
  --yes \
   \
  --watch \
  --machine-cidr "10.30.0.0/16" \
  --service-cidr "172.32.0.0/16" \
  --pod-cidr "10.136.0.0/14" \
  --host-prefix 23 \
  --subnet-ids "subnet-05230c2da243c5e92,subnet-0b9be681e23cfa37a" \
  --replicas 3 \
  --compute-machine-type "m5.xlarge" \
  --channel-group stable &
primary_cluster_pid=$!

create_or_wait_cluster "$SECONDARY_CLUSTER_NAME" \
  --cluster-name "$SECONDARY_CLUSTER_NAME" \
  --region "us-west-2" \
  --version "4.19.27" \
  --sts \
  --mode auto \
  --yes \
   \
  --watch \
  --machine-cidr "10.40.0.0/16" \
  --service-cidr "172.33.0.0/16" \
  --pod-cidr "10.140.0.0/14" \
  --host-prefix 23 \
  --subnet-ids "subnet-07488782f03ceea47,subnet-0857afa7da203e833" \
  --replicas 3 \
  --compute-machine-type "m5.xlarge" \
  --channel-group stable &
secondary_cluster_pid=$!

wait "$primary_cluster_pid"
wait "$secondary_cluster_pid"

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

primary_infra_id="$(rosa describe cluster -c "test-bed-419-east" -o json | jq -r '.infra_id')"
secondary_infra_id="$(rosa describe cluster -c "test-bed-419-west" -o json | jq -r '.infra_id')"

primary_worker_sg="$(find_node_sg "us-east-2" "vpc-0203c4abe307ce9c2" "$primary_infra_id")"
secondary_worker_sg="$(find_node_sg "us-west-2" "vpc-081053c59a01c2fdb" "$secondary_infra_id")"

aws ec2 authorize-security-group-ingress \
  --region "us-east-2" \
  --group-id "$primary_worker_sg" \
  --ip-permissions '[
    {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"10.40.0.0/16","Description":"Consul RPC from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"10.40.0.0/16","Description":"Consul gossip TCP from secondary ROSA VPC"}]},
    {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"10.40.0.0/16","Description":"Consul gossip UDP from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"10.40.0.0/16","Description":"Consul HTTPS from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"10.40.0.0/16","Description":"Consul gRPC TLS from secondary ROSA VPC"}]}
  ]' >/dev/null 2>&1 || true

aws ec2 authorize-security-group-ingress \
  --region "us-west-2" \
  --group-id "$secondary_worker_sg" \
  --ip-permissions '[
    {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"10.30.0.0/16","Description":"Consul RPC from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"10.30.0.0/16","Description":"Consul gossip TCP from primary ROSA VPC"}]},
    {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"10.30.0.0/16","Description":"Consul gossip UDP from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"10.30.0.0/16","Description":"Consul HTTPS from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"10.30.0.0/16","Description":"Consul gRPC TLS from primary ROSA VPC"}]}
  ]' >/dev/null 2>&1 || true

echo "ROSA cluster pair rosa419 is ready"
