# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    azurerm = {
      version = "3.40.0"
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

resource "random_password" "winnode" {
  length = 16
}

resource "azurerm_kubernetes_cluster" "default" {
  count                             = var.cluster_count
  name                              = "consul-k8s-${random_id.suffix[count.index].dec}"
  location                          = azurerm_resource_group.default[count.index].location
  resource_group_name               = azurerm_resource_group.default[count.index].name
  dns_prefix                        = "consul-k8s-${random_id.suffix[count.index].dec}"
  kubernetes_version                = var.kubernetes_version
  role_based_access_control_enabled = true

  // We're setting the network plugin to azure since it supports windows node pools
  network_profile {
    network_plugin = "azure"
  }

  default_node_pool {
    name            = "default"
    node_count      = 3
    vm_size         = "Standard_D3_v2"
    os_disk_size_gb = 30
    vnet_subnet_id  = azurerm_virtual_network.default[count.index].subnet.*.id[0]
  }

  windows_profile {
    admin_username = "azadmin"
    admin_password = random_password.winnode.result
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

resource "azurerm_kubernetes_cluster_node_pool" "win" {
  count                 = var.windows ? var.cluster_count : 0
  name                  = "win"
  kubernetes_cluster_id = azurerm_kubernetes_cluster.default[count.index].id
  vm_size               = "Standard_DS2_v2"
  node_count            = 1
  os_type               = "Windows"
  os_sku                = "Windows2019"
}