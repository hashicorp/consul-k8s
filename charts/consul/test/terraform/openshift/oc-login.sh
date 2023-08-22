#!/usr/bin/env bash

resource_group=$1
cluster_name=$2

apiServer=$(az aro show -g "$resource_group" -n "$cluster_name" --query apiserverProfile.url -o tsv)
kubeUser=$(az aro list-credentials -g "$resource_group" -n "$cluster_name" | jq -r .kubeadminUsername)
kubePassword=$(az aro list-credentials -g "$resource_group" -n "$cluster_name" | jq -r .kubeadminPassword)

echo "Logging in"
for i in {1..20}; do KUBECONFIG="$HOME/.kube/$cluster_name" oc login "$apiServer" -u "$kubeUser" -p "$kubePassword" && break; sleep 5; done

echo "Creating the 'consul' project"
# Idempotently, create and use the 'consul' project
KUBECONFIG="$HOME/.kube/$cluster_name" oc new-project consul
KUBECONFIG="$HOME/.kube/$cluster_name" oc project consul

echo "Disabling auto-update of the worker nodes based on config changes"
KUBECONFIG="$HOME/.kube/$cluster_name" kubectl patch --type=merge --patch='{"spec":{"paused":true}}' machineconfigpool/worker