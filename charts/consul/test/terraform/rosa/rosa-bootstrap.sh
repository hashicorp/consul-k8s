#!/usr/bin/env bash
set -euo pipefail

PRIMARY_CLUSTER_NAME="test-bed-east-tf"
SECONDARY_CLUSTER_NAME="test-bed-west-tf"
PRIMARY_REGION="us-east-2"
SECONDARY_REGION="us-west-2"
PRIMARY_VPC_ID="vpc-087a1fbff644aa939"
SECONDARY_VPC_ID="vpc-0f3d0b8e78a6bf1b8"
PRIMARY_VPC_CIDR="10.10.0.0/16"
SECONDARY_VPC_CIDR="10.20.0.0/16"

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
  --version "4.19.26" \
  --sts \
  --mode auto \
  --yes \
   \
  --watch \
  --machine-cidr "10.10.0.0/16" \
  --service-cidr "172.30.0.0/16" \
  --pod-cidr "10.128.0.0/14" \
  --host-prefix 23 \
  --subnet-ids "subnet-0648f9f512197f31a,subnet-0268c5bcffaf51f86" \
  --replicas 2 \
  --compute-machine-type "m5.xlarge" \
  --channel-group stable &
primary_cluster_pid=$!

create_or_wait_cluster "$SECONDARY_CLUSTER_NAME" \
  --cluster-name "$SECONDARY_CLUSTER_NAME" \
  --region "$SECONDARY_REGION" \
  --version "4.19.26" \
  --sts \
  --mode auto \
  --yes \
   \
  --watch \
  --machine-cidr "10.20.0.0/16" \
  --service-cidr "172.31.0.0/16" \
  --pod-cidr "10.132.0.0/14" \
  --host-prefix 23 \
  --subnet-ids "subnet-07df0078c4d40da23,subnet-059a1ad62dd0feb65" \
  --replicas 2 \
  --compute-machine-type "m5.xlarge" \
  --channel-group stable &
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
    {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"10.20.0.0/16","Description":"Consul RPC from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"10.20.0.0/16","Description":"Consul gossip TCP from secondary ROSA VPC"}]},
    {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"10.20.0.0/16","Description":"Consul gossip UDP from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"10.20.0.0/16","Description":"Consul HTTPS from secondary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"10.20.0.0/16","Description":"Consul gRPC TLS from secondary ROSA VPC"}]}
  ]' >/dev/null 2>&1 || true

aws ec2 authorize-security-group-ingress \
  --region "$SECONDARY_REGION" \
  --group-id "$secondary_worker_sg" \
  --ip-permissions '[
    {"IpProtocol":"tcp","FromPort":8300,"ToPort":8300,"IpRanges":[{"CidrIp":"10.10.0.0/16","Description":"Consul RPC from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"10.10.0.0/16","Description":"Consul gossip TCP from primary ROSA VPC"}]},
    {"IpProtocol":"udp","FromPort":8301,"ToPort":8301,"IpRanges":[{"CidrIp":"10.10.0.0/16","Description":"Consul gossip UDP from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8501,"ToPort":8501,"IpRanges":[{"CidrIp":"10.10.0.0/16","Description":"Consul HTTPS from primary ROSA VPC"}]},
    {"IpProtocol":"tcp","FromPort":8502,"ToPort":8502,"IpRanges":[{"CidrIp":"10.10.0.0/16","Description":"Consul gRPC TLS from primary ROSA VPC"}]}
  ]' >/dev/null 2>&1 || true

echo "ROSA clusters are ready and worker security group ingress has been applied"