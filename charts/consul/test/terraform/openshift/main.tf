provider "azurerm" {
  features {}
}

provider "azuread" {
  version = "1.4.0"
}

data "azurerm_client_config" "current" {}

resource "random_id" "suffix" {
  count       = var.cluster_count
  byte_length = 4
}

resource "random_string" "domain" {
  count   = var.cluster_count
  special = false
  number  = false
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
  count                                         = var.cluster_count
  name                                          = "master-subnet"
  resource_group_name                           = azurerm_resource_group.test[count.index].name
  virtual_network_name                          = azurerm_virtual_network.test[count.index].name
  address_prefixes                              = ["10.0.0.0/23"]
  enforce_private_link_service_network_policies = true
  service_endpoints                             = ["Microsoft.ContainerRegistry"]
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
}

resource "azuread_service_principal" "test" {
  count          = var.cluster_count
  application_id = azuread_application.test[count.index].application_id
}

resource "random_password" "password" {
  count   = var.cluster_count
  length  = 16
  special = false
}

resource "azuread_service_principal_password" "test" {
  count                = var.cluster_count
  service_principal_id = azuread_service_principal.test[count.index].id
  value                = random_password.password[count.index].result
  end_date             = "2099-01-01T01:02:03Z"
}

data "azuread_service_principal" "openshift_rp" {
  application_id = local.openshift_resource_provider_application_id
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

resource "azurerm_template_deployment" "azure-arocluster" {
  count               = var.cluster_count
  name                = azurerm_resource_group.test[count.index].name
  resource_group_name = azurerm_resource_group.test[count.index].name

  template_body = file("./aro-template.json")

  parameters = {
    clusterName              = azurerm_resource_group.test[count.index].name
    clusterResourceGroupName = join("-", [azurerm_resource_group.test[count.index].name, "MANAGED"])
    location                 = var.location

    tags = jsonencode(var.tags)

    domain = random_string.domain[count.index].result

    clientId     = azuread_application.test[count.index].application_id
    clientSecret = azuread_service_principal_password.test[count.index].value

    workerSubnetId = azurerm_subnet.worker-subnet[count.index].id
    masterSubnetId = azurerm_subnet.master-subnet[count.index].id
  }

  timeouts {
    create = "90m"
  }

  deployment_mode = "Incremental"

  depends_on = [
    azurerm_role_assignment.rp_assignment,
    azurerm_role_assignment.vnet_assignment,
  ]
}

resource "null_resource" "aro" {
  count = var.cluster_count

  triggers = {
    aro_cluster = azurerm_template_deployment.azure-arocluster[count.index].name
  }

  provisioner "local-exec" {
    command     = "./oc-login.sh ${self.triggers.aro_cluster} ${self.triggers.aro_cluster}"
    interpreter = ["/bin/bash", "-c"]
    on_failure  = continue
  }

  // We need to explicitly delete the cluster in a local-exec because deleting azurerm_template_deployment
  // will not delete the cluster.
  provisioner "local-exec" {
    when    = destroy
    command = "az aro delete --resource-group ${self.triggers.aro_cluster} --name ${self.triggers.aro_cluster} --yes"
  }
}