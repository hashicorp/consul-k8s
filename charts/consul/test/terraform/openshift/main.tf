# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    azurerm = {
      version = "3.90.0"
    }
    azuread = {
      version = "2.47.0"
    }
  }
}

provider "azurerm" {
  features {}
}

provider "azuread" {}

data "azurerm_client_config" "current" {}

resource "random_id" "suffix" {
  count       = var.cluster_count
  byte_length = 4
}

resource "random_string" "domain" {
  count   = var.cluster_count
  special = false
  numeric = false
  upper   = false
  length  = 6
}

locals {
  // This is the application ID of the Azure RedHat OpenShift resource provider.
  // This is a constant for this resource provider.
  openshift_resource_provider_application_id = "f1dd0a37-89c6-4e07-bcd1-ffd3d43d8875"
}

resource "azurerm_resource_group" "test" {
  count    = var.cluster_count
  name     = "consul-k8s-${random_id.suffix[count.index].dec}"
  location = var.location
}
resource "azurerm_virtual_network" "test" {
  count               = var.cluster_count
  name                = "consul-k8s-${random_id.suffix[count.index].dec}"
  location            = azurerm_resource_group.test[count.index].location
  resource_group_name = azurerm_resource_group.test[count.index].name
  address_space       = ["10.0.0.0/22"]
}

resource "azurerm_subnet" "master-subnet" {
  count                = var.cluster_count
  name                 = "master-subnet"
  resource_group_name  = azurerm_resource_group.test[count.index].name
  virtual_network_name = azurerm_virtual_network.test[count.index].name
  address_prefixes     = ["10.0.0.0/23"]
  service_endpoints    = ["Microsoft.ContainerRegistry"]
}

resource "azurerm_subnet" "worker-subnet" {
  count                = var.cluster_count
  name                 = "worker-subnet"
  resource_group_name  = azurerm_resource_group.test[count.index].name
  virtual_network_name = azurerm_virtual_network.test[count.index].name
  address_prefixes     = ["10.0.2.0/23"]
  service_endpoints    = ["Microsoft.ContainerRegistry"]
}

resource "azuread_application" "test" {
  count        = var.cluster_count
  display_name = "aro-consul-k8s-${random_id.suffix[count.index].dec}"
  owners       = [data.azurerm_client_config.current.object_id]
}

resource "azuread_service_principal" "test" {
  count     = var.cluster_count
  client_id = azuread_application.test[count.index].application_id
  owners    = [data.azurerm_client_config.current.object_id]
}

resource "random_password" "password" {
  count   = var.cluster_count
  length  = 16
  special = false
}

resource "azuread_service_principal_password" "test" {
  count                = var.cluster_count
  service_principal_id = azuread_service_principal.test[count.index].id
  end_date             = "2099-01-01T01:02:03Z"
}

data "azuread_service_principal" "openshift_rp" {
  client_id = local.openshift_resource_provider_application_id
}

resource "azurerm_role_assignment" "vnet_assignment" {
  count                = var.cluster_count
  scope                = azurerm_virtual_network.test[count.index].id
  role_definition_name = "Network Contributor"
  principal_id         = azuread_service_principal.test[count.index].object_id
}

resource "azurerm_role_assignment" "rp_assignment" {
  count                = var.cluster_count
  scope                = azurerm_virtual_network.test[count.index].id
  role_definition_name = "Network Contributor"
  principal_id         = data.azuread_service_principal.openshift_rp.object_id
}

resource "azurerm_redhat_openshift_cluster" "azure_arocluster" {
  count               = var.cluster_count
  name                = azurerm_resource_group.test[count.index].name
  location            = var.location
  resource_group_name = azurerm_resource_group.test[count.index].name

  cluster_profile {
    domain  = random_string.domain[count.index].result
    version = "4.13.23"
  }

  network_profile {
    pod_cidr     = "10.128.0.0/14"
    service_cidr = "172.30.0.0/16"
  }

  main_profile {
    vm_size   = "Standard_D8s_v3"
    subnet_id = azurerm_subnet.master-subnet[count.index].id
  }

  api_server_profile {
    visibility = "Public"
  }

  ingress_profile {
    visibility = "Public"
  }

  worker_profile {
    vm_size      = "Standard_D4s_v3"
    disk_size_gb = 128
    node_count   = 3
    subnet_id    = azurerm_subnet.worker-subnet[count.index].id
  }

  service_principal {
    client_id     = azuread_application.test[count.index].application_id
    client_secret = azuread_service_principal_password.test[count.index].value
  }

  depends_on = [
    azurerm_role_assignment.rp_assignment,
    azurerm_role_assignment.vnet_assignment,
  ]

}
