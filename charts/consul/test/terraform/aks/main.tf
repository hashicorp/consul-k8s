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

  // We're setting the network plugin and other network properties explicitly
  // here even though they are the same as defaults to ensure that none of these CIDRs
  // overlap with our vnet and subnet. Please see
  // https://docs.microsoft.com/en-us/azure/aks/configure-kubenet#create-an-aks-cluster-in-the-virtual-network.
  // We want to use kubenet plugin rather than Azure CNI because pods
  // using kubenet will not be routable when we peer VNets,
  // and that gives us more confidence that in any tests where cross-cluster
  // communication is tested, the connections goes through the appropriate gateway
  // rather than directly from pod to pod.
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
