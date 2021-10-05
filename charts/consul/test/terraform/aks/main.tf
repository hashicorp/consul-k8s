provider "azurerm" {
  version = "2.78.0"
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

resource "azurerm_kubernetes_cluster" "default" {
  count               = var.cluster_count
  name                = "consul-k8s-${random_id.suffix[count.index].dec}"
  location            = azurerm_resource_group.default[count.index].location
  resource_group_name = azurerm_resource_group.default[count.index].name
  dns_prefix          = "consul-k8s-${random_id.suffix[count.index].dec}"
  kubernetes_version  = "1.20.9"

  default_node_pool {
    name            = "default"
    node_count      = 3
    vm_size         = "Standard_D2_v2"
    os_disk_size_gb = 30
  }

  service_principal {
    client_id     = var.client_id
    client_secret = var.client_secret
  }

  role_based_access_control {
    enabled = true
  }

  tags = var.tags
}

resource "local_file" "kubeconfigs" {
  count           = var.cluster_count
  content         = azurerm_kubernetes_cluster.default[count.index].kube_config_raw
  filename        = pathexpand("~/.kube/consul-k8s-${random_id.suffix[count.index].dec}")
  file_permission = "0600"
}
