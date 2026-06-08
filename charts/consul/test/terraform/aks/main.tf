# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    azurerm = {
      version = "~> 4.33.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.5.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12.0"
    }
  }
}

provider "azurerm" {
  features {}
}

provider "local" {}

resource "random_id" "suffix" {
  count       = var.cluster_count
  byte_length = 4
}

resource "azurerm_resource_group" "default" {
  count    = var.cluster_count
  name     = "consul-k8s-${random_id.suffix[count.index].dec}"
  location = var.location

  tags = var.tags
}

resource "azurerm_virtual_network" "default" {
  count               = var.cluster_count
  name                = "consul-k8s-${random_id.suffix[count.index].dec}"
  location            = azurerm_resource_group.default[count.index].location
  resource_group_name = azurerm_resource_group.default[count.index].name
  address_space       = ["192.${count.index + 168}.0.0/16"]

  subnet {
    name             = "consul-k8s-${random_id.suffix[count.index].dec}-subnet"
    address_prefixes = ["192.${count.index + 168}.1.0/24"]
  }
}

resource "azurerm_virtual_network_peering" "default" {
  count                     = var.cluster_count > 1 ? var.cluster_count : 0
  name                      = "peering-${count.index}"
  resource_group_name       = azurerm_resource_group.default[count.index].name
  virtual_network_name      = azurerm_virtual_network.default[count.index].name
  remote_virtual_network_id = azurerm_virtual_network.default[count.index == 0 ? 1 : 0].id
}

resource "azurerm_kubernetes_cluster" "default" {
  count                             = var.cluster_count
  name                              = "consul-k8s-${random_id.suffix[count.index].dec}"
  location                          = azurerm_resource_group.default[count.index].location
  resource_group_name               = azurerm_resource_group.default[count.index].name
  dns_prefix                        = "consul-k8s-${random_id.suffix[count.index].dec}"
  kubernetes_version                = var.kubernetes_version
  role_based_access_control_enabled = true

  // We set the network plugin and CIDRs explicitly (even where they match the
  // defaults) to ensure none of the pod/service CIDRs overlap with the VNet
  // and subnet defined above.
  //
  // We use Azure CNI in *overlay* mode (network_plugin = "azure" with
  // network_plugin_mode = "overlay"). Previously this configuration used
  // kubenet, but kubenet was replaced for the following reasons:
  //
  //   1. On AKS v1.35, several partition-related acceptance tests started
  //      failing. The partition-init container calls the consul-server API,
  //      and the consul-server pod in turn tries to reach itself over RPC on
  //      port 8300; that pod-to-self connection was not reachable on 1.35
  //      with kubenet. 
  //      A similar symptom is reported in https://github.com/Azure/AKS/issues/5669. 
  //      The root cause is not yet confirmed (it may or may not be kubenet itself); 
  //      switching to Azure CNI overlay made all the failing partition tests pass.
  //      TODO: track down the actual root cause on AKS 1.35 + kubenet.
  //
  //   2. Kubenet is deprecated in AKS and scheduled for retirement by 2028.
  //      Even before then, Azure _may_restrict_ its use for newly created
  //      clusters, so moving off it now is a good idea regardless.
  //
  // Why overlay specifically (and not standard Azure CNI): the original
  // reason for choosing kubenet was that kubenet pod IPs are not routable
  // across peered VNets, which forces cross-cluster test traffic to go
  // through the mesh/mesh-gateway rather than pod-to-pod directly. 
  // Azure CNI overlay preserves this property — pods get IPs from a private pod_cidr
  // that is not advertised to the VNet, and egress to peered VNets is SNAT'd
  // to the node IP. Standard (non-overlay) Azure CNI would assign pod IPs
  // from the VNet subnet and make them routable across peering, which would
  // invalidate those tests.
  //
  // Refs:
  //   https://learn.microsoft.com/azure/aks/azure-cni-overlay
  //   https://learn.microsoft.com/en-us/azure/aks/concepts-network-azure-cni-overlay

  network_profile {
    network_plugin      = "azure"
    network_plugin_mode = "overlay"
    network_policy      = "calico"
    service_cidr        = "10.0.0.0/16"
    dns_service_ip      = "10.0.0.10"
    pod_cidr            = "10.244.0.0/16"
  }

  default_node_pool {
    name            = "default"
    node_count      = 3
    vm_size         = "Standard_D3_v2"
    os_disk_size_gb = 30
    vnet_subnet_id  = azurerm_virtual_network.default[count.index].subnet.*.id[0]
  }

  identity {
    type = "SystemAssigned"
  }

  tags = var.tags
}

resource "local_file" "kubeconfigs" {
  count           = var.cluster_count
  content         = azurerm_kubernetes_cluster.default[count.index].kube_config_raw
  filename        = pathexpand("~/.kube/consul-k8s-${random_id.suffix[count.index].dec}")
  file_permission = "0600"
}

# Deploy IBM Uptycs EDR agent to each AKS cluster.
provider "helm" {
  alias = "cluster_0"
  kubernetes {
    host                   = azurerm_kubernetes_cluster.default[0].kube_config[0].host
    client_certificate     = base64decode(azurerm_kubernetes_cluster.default[0].kube_config[0].client_certificate)
    client_key             = base64decode(azurerm_kubernetes_cluster.default[0].kube_config[0].client_key)
    cluster_ca_certificate = base64decode(azurerm_kubernetes_cluster.default[0].kube_config[0].cluster_ca_certificate)
  }
}

provider "helm" {
  alias = "cluster_1"
  kubernetes {
    host                   = var.cluster_count > 1 ? azurerm_kubernetes_cluster.default[1].kube_config[0].host : azurerm_kubernetes_cluster.default[0].kube_config[0].host
    client_certificate     = base64decode(var.cluster_count > 1 ? azurerm_kubernetes_cluster.default[1].kube_config[0].client_certificate : azurerm_kubernetes_cluster.default[0].kube_config[0].client_certificate)
    client_key             = base64decode(var.cluster_count > 1 ? azurerm_kubernetes_cluster.default[1].kube_config[0].client_key : azurerm_kubernetes_cluster.default[0].kube_config[0].client_key)
    cluster_ca_certificate = base64decode(var.cluster_count > 1 ? azurerm_kubernetes_cluster.default[1].kube_config[0].cluster_ca_certificate : azurerm_kubernetes_cluster.default[0].kube_config[0].cluster_ca_certificate)
  }
}

# Cluster 0 EDR
resource "helm_release" "uptycs_0" {
  provider         = helm.cluster_0
  name             = "k8sosquery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "k8sosquery"
  namespace        = "uptycs"
  create_namespace = true
  cleanup_on_fail  = true

  values = [
    templatefile("${path.module}/k8sosquery-values.yaml", {
      owner         = var.uptycs_owner
      enroll_secret = var.uptycs_enroll_secret
    })
  ]
}

resource "helm_release" "kubequery_0" {
  depends_on       = [helm_release.uptycs_0]
  provider         = helm.cluster_0
  name             = "kubequery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "kubequery"
  namespace        = "kubequery"
  create_namespace = true
  cleanup_on_fail  = true

  set {
    name  = "deployment.spec.hostname"
    value = azurerm_kubernetes_cluster.default[0].name
  }

  values = [
    templatefile("${path.module}/kubequery-values.yaml", {
      enroll_secret     = var.uptycs_enroll_secret
      webhook_ca_bundle = var.uptycs_webhook_ca_bundle
      webhook_tls_crt   = var.uptycs_webhook_tls_crt
      webhook_tls_key   = var.uptycs_webhook_tls_key
    })
  ]
}

# Cluster 1 EDR (only when cluster_count > 1)
resource "helm_release" "uptycs_1" {
  count            = var.cluster_count > 1 ? 1 : 0
  provider         = helm.cluster_1
  name             = "k8sosquery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "k8sosquery"
  namespace        = "uptycs"
  create_namespace = true
  cleanup_on_fail  = true

  values = [
    templatefile("${path.module}/k8sosquery-values.yaml", {
      owner         = var.uptycs_owner
      enroll_secret = var.uptycs_enroll_secret
    })
  ]
}

resource "helm_release" "kubequery_1" {
  count            = var.cluster_count > 1 ? 1 : 0
  depends_on       = [helm_release.uptycs_1]
  provider         = helm.cluster_1
  name             = "kubequery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "kubequery"
  namespace        = "kubequery"
  create_namespace = true
  cleanup_on_fail  = true

  set {
    name  = "deployment.spec.hostname"
    value = azurerm_kubernetes_cluster.default[1].name
  }

  values = [
    templatefile("${path.module}/kubequery-values.yaml", {
      enroll_secret     = var.uptycs_enroll_secret
      webhook_ca_bundle = var.uptycs_webhook_ca_bundle
      webhook_tls_crt   = var.uptycs_webhook_tls_crt
      webhook_tls_key   = var.uptycs_webhook_tls_key
    })
  ]
}
