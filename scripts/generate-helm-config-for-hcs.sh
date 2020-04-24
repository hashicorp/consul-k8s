#!/usr/bin/env bash

############################################################################
# WARNING: This script is experimental and is not meant for production use #
############################################################################
#
# Script to retrieve configuration from Hashicorp Consul Service on Azure,
# bootstrap ACLs, and create Kubernetes secrets and Helm config file
# to install this Helm chart.

set -e

: "${subscription_id?subscription_id environment variable required}"
: "${resource_group?resource_group environment variable required}"
: "${managed_app_name?managed_app_name environment variable required}"
: "${cluster_name?cluster_name environment variable required}"

echo "-> Fetching cluster configuration from Azure"
cluster_resource=$(az resource show --ids "/subscriptions/${subscription_id}/resourceGroups/${resource_group}/providers/Microsoft.Solutions/applications/${managed_app_name}/customconsulClusters/${cluster_name}" --api-version 2018-09-01-preview)
cluster_config_file_base64=$(echo "${cluster_resource}" | jq -r .properties.consulConfigFile)
ca_file_base64=$(echo "${cluster_resource}" | jq -r .properties.consulCaFile)

echo "Writing cluster configuration to consul.json"
echo "${cluster_config_file_base64}" | base64 --decode | jq . > consul.json

echo "Writing CA certificate chain to ca.pem"
echo "${ca_file_base64}" | base64 --decode > ca.pem
echo

echo "-> Bootstrapping ACLs"

# Extract the URL for the servers.
# First, check if the external endpoint is enabled and if yes, use the external endpoint URL.
# Otherwise, use the private endpoint URL.
external_endpoint_enabled=$(echo "${cluster_resource}" | jq -r .properties.consulExternalEndpoint)
if [ "$external_endpoint_enabled" == "enabled" ]; then
  server_url=$(echo "${cluster_resource}" | jq -r .properties.consulExternalEndpointUrl)
else
  server_url=$(echo "${cluster_resource}" | jq -r .properties.consulPrivateEndpointUrl)
fi

# Call Consul bootstrap API and save the bootstrap secret
# to a Kubernetes secret if successful.
output=$(curl -sX PUT "${server_url}"/v1/acl/bootstrap)
if grep -i "permission denied" <<< "$output"; then
  echo "ACL system already bootstrapped."
  echo "Please update 'global.acls.bootstrapToken' values in the generated Helm config to point to the Kubernetes secret containing the bootstrap token."
elif  grep -i "ACL support disabled" <<< "$output"; then
  echo "ACLs not enabled on this cluster."
  exit 1
else
  echo "Successfully bootstrapped ACLs"
  echo "Creating Kubernetes secret for the bootstrap token ${managed_app_name}-bootstrap-token"
  kubectl create secret generic "${managed_app_name}"-bootstrap-token \
          --from-literal="token=$(echo "${output}" | jq -r .SecretID)"
fi

echo
echo "-> Creating Kubernetes secret ${managed_app_name}-consul-ca-cert"
kubectl create secret generic "${managed_app_name}"-consul-ca-cert --from-file='tls.crt=./ca.pem'

echo
echo "-> Creating Kubernetes secret ${managed_app_name}-gossip-key"
gossip_key=$(jq -r .encrypt consul.json)
kubectl create secret generic "${managed_app_name}"-gossip-key --from-literal=key="${gossip_key}"

retry_join=$(jq -r --compact-output .retry_join consul.json)
kube_api_server=$(kubectl config view -o jsonpath="{.clusters[?(@.name == \"$(kubectl config current-context)\")].cluster.server}")
consul_version=$(echo "${cluster_resource}" | jq -r .properties.consulInitialVersion | cut -d'v' -f2)

echo
echo "-> Writing Helm config to config.yaml"
cat > config.yaml << EOF
global:
  enabled: false
  image: hashicorp/consul-enterprise:${consul_version}-ent
  datacenter: $(jq -r .datacenter consul.json)
  acls:
    manageSystemACLs: true
    bootstrapToken:
      secretName: ${managed_app_name}-bootstrap-token
      secretKey: token
  gossipEncryption:
    secretName: ${managed_app_name}-gossip-key
    secretKey: key
  tls:
    enabled: true
    enableAutoEncrypt: true
    caCert:
      secretName: ${managed_app_name}-consul-ca-cert
      secretKey: tls.crt
externalServers:
  enabled: true
  hosts: ${retry_join}
  httpsPort: 443
  useSystemRoots: true
  k8sAuthMethodHost: ${kube_api_server}
client:
  enabled: true
  # If you are using Kubenet in your AKS cluster,
  # uncomment the line below.
  # exposeGossipPorts: true
connectInject:
  enabled: true
syncCatalog:
  enabled: true
EOF

echo
echo "Done"