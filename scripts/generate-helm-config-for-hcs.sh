#!/usr/bin/env bash

############################################################################
# WARNING: This script is experimental and is not meant for production use #
############################################################################
#
# Script to retrieve configuration from Hashicorp Consul Service on Azure,
# bootstrap ACLs, and create Kubernetes secrets and Helm config file
# to install this Helm chart.

set -euo pipefail

: "${subscription_id?subscription_id environment variable required}"
: "${resource_group?resource_group environment variable required}"
: "${managed_app_name?managed_app_name environment variable required}"
: "${cluster_name?cluster_name environment variable required}"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;93m'
NOCOLOR='\033[0m'

echo -e "${YELLOW}-> Fetching cluster configuration from Azure${NOCOLOR}"
cluster_resource=$(az resource show --ids "/subscriptions/${subscription_id}/resourceGroups/${resource_group}/providers/Microsoft.Solutions/applications/${managed_app_name}/customconsulClusters/${cluster_name}" --api-version 2018-09-01-preview)
cluster_config_file_base64=$(echo "${cluster_resource}" | jq -r .properties.consulConfigFile)
ca_file_base64=$(echo "${cluster_resource}" | jq -r .properties.consulCaFile)

echo "Writing cluster configuration to consul.json"
echo "${cluster_config_file_base64}" | base64 --decode | jq . > consul.json

echo "Writing CA certificate chain to ca.pem"
echo "${ca_file_base64}" | base64 --decode > ca.pem
echo

echo -e "${YELLOW}-> Bootstrapping ACLs${NOCOLOR}"

# Extract the URL for the servers.
# First, check if the external endpoint is enabled and if yes, use the external endpoint URL.
# Otherwise, use the private endpoint URL.
external_endpoint_enabled=$(echo "${cluster_resource}" | jq -r .properties.consulExternalEndpoint)
if [ "$external_endpoint_enabled" == "enabled" ]; then
  server_url=$(echo "${cluster_resource}" | jq -r .properties.consulExternalEndpointUrl)
else
  server_url=$(echo "${cluster_resource}" | jq -r .properties.consulPrivateEndpointUrl)
fi

# Use managed_app_name as a resource prefix for Kubernetes resources
# We need to convert it to lower case because of Kubernetes resource restrictions.
# https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
kube_resource_prefix=$(echo "${managed_app_name}" | tr '[:upper:]' '[:lower:]')

# Call Consul bootstrap API and save the bootstrap secret
# to a Kubernetes secret if successful.
output=$(curl --connect-timeout 30 -sSX PUT "${server_url}"/v1/acl/bootstrap)
if grep -i "permission denied" <<< "$output"; then
  echo "ACL system already bootstrapped."
  echo -e "${RED}Please update 'global.acls.bootstrapToken' values in the generated Helm config to point to the Kubernetes secret containing the bootstrap token.${NOCOLOR}"
  echo -e "${RED}You can create a secret like so:${NOCOLOR}"
  echo -e "${RED}kubectl create secret generic ${kube_resource_prefix}-bootstrap-token \ ${NOCOLOR}"
  echo -e "${RED}   --from-literal='token=<your bootstrap secret>'${NOCOLOR}"
elif  grep -i "ACL support disabled" <<< "$output"; then
  echo -e "${RED}ACLs not enabled on this cluster.${NOCOLOR}"
  exit 1
else
  echo "Successfully bootstrapped ACLs. Writing ACL bootstrap output to acls.json"
  echo "$output" > acls.json

  echo "Creating Kubernetes secret for the bootstrap token ${kube_resource_prefix}-bootstrap-token"
  kubectl create secret generic "${kube_resource_prefix}"-bootstrap-token \
          --from-literal="token=$(echo "${output}" | jq -r .SecretID)"
fi

echo
echo -e "${YELLOW}-> Creating Kubernetes secret ${kube_resource_prefix}-consul-ca-cert${NOCOLOR}"
kubectl create secret generic "${kube_resource_prefix}"-consul-ca-cert --from-file='tls.crt=./ca.pem'

echo
echo -e "${YELLOW}-> Creating Kubernetes secret ${kube_resource_prefix}-gossip-key${NOCOLOR}"
gossip_key=$(jq -r .encrypt consul.json)
kubectl create secret generic "${kube_resource_prefix}"-gossip-key --from-literal=key="${gossip_key}"

retry_join=$(jq -r --compact-output .retry_join consul.json)
kube_api_server=$(kubectl config view -o jsonpath="{.clusters[?(@.name == \"$(kubectl config current-context)\")].cluster.server}")

echo
echo -e "${YELLOW}-> Writing Helm config to config.yaml${NOCOLOR}"
cat > config.yaml << EOF
global:
  enabled: false
  name: consul
  datacenter: $(jq -r .datacenter consul.json)
  acls:
    manageSystemACLs: true
    bootstrapToken:
      secretName: ${kube_resource_prefix}-bootstrap-token
      secretKey: token
  gossipEncryption:
    secretName: ${kube_resource_prefix}-gossip-key
    secretKey: key
  tls:
    enabled: true
    enableAutoEncrypt: true
    caCert:
      secretName: ${kube_resource_prefix}-consul-ca-cert
      secretKey: tls.crt
externalServers:
  enabled: true
  hosts: ${retry_join}
  httpsPort: 443
  useSystemRoots: true
  k8sAuthMethodHost: ${kube_api_server}
client:
  enabled: true
  # If you are using Kubenet in your AKS cluster (the default network),
  # uncomment the line below.
  # exposeGossipPorts: true
  join: ${retry_join}
connectInject:
  enabled: true
EOF

echo
echo -e "${GREEN}Done${GREEN}"
