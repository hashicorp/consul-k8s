# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    azurerm = {
      version = "~> 4.27.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.5.0"
    }
  }
}

provider "azurerm" {
  features {}
  skip_provider_registration = true
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
    name           = "consul-k8s-${random_id.suffix[count.index].dec}-subnet"
    address_prefix = "192.${count.index + 168}.1.0/24"
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
    network_plugin     = "kubenet"
    service_cidr       = "10.0.0.0/16"
    dns_service_ip     = "10.0.0.10"
    pod_cidr           = "10.244.0.0/16"
    docker_bridge_cidr = "172.17.0.1/16"
  }

  default_node_pool {
    name            = "default"
    node_count      = 3
    vm_size         = "Standard_D3_v2"
    os_disk_size_gb = 30
    vnet_subnet_id  = azurerm_virtual_network.default[count.index].subnet.*.id[0]
  }

  service_principal {
    client_id     = var.client_id
    client_secret = var.client_secret
  }

  tags = var.tags
}

resource "local_file" "kubeconfigs" {
  count           = var.cluster_count
  content         = azurerm_kubernetes_cluster.default[count.index].kube_config_raw
  filename        = pathexpand("~/.kube/consul-k8s-${random_id.suffix[count.index].dec}")
  file_permission = "0600"
}
