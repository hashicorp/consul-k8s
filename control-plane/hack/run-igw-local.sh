#!/usr/bin/env bash
# run-igw-local.sh — Run connect-inject out-of-cluster for InferenceGateway testing
# Usage: bash control-plane/hack/run-igw-local.sh
set -euo pipefail

export PATH=$PATH:/opt/homebrew/bin:/usr/local/go/bin

BINARY=${BINARY:-/tmp/connect-inject}
CONSUL_HTTP_ADDR=${CONSUL_HTTP_ADDR:-127.0.0.1:8500}
CONSUL_GRPC_ADDR=${CONSUL_GRPC_ADDR:-127.0.0.1:8502}
KUBECONFIG=${KUBECONFIG:-$HOME/.kube/config}
CONTEXT=${CONTEXT:-kind-cluster-1}
DATACENTER=${DATACENTER:-dc1}
IMAGE=${IMAGE:-hashicorp/consul:latest}       # placeholder — never pulled in this test

echo "▶ Binary      : $BINARY"
echo "▶ Consul HTTP : $CONSUL_HTTP_ADDR"
echo "▶ Consul gRPC : $CONSUL_GRPC_ADDR"
echo "▶ K8s context : $CONTEXT"
echo "▶ Datacenter  : $DATACENTER"
echo ""

kubectl config use-context "$CONTEXT"

exec "$BINARY" inject-connect \
  -enable-ai \
  -ai-inference-gateway-image="$IMAGE" \
  -datacenter="$DATACENTER" \
  -http-port=8500 \
  -grpc-port=8502 \
  -consul-api-timeout=10s \
  -resource-prefix=consul \
  -allow-k8s-namespace="*" \
  -deny-k8s-namespace="" \
  -listen=":0" \
  -enable-webhook-ca-update=false \
  2>&1
