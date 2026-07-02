# Copyright IBM Corp. 2018, 2025
# SPDX-License-Identifier: MPL-2.0

output "kubeconfigs" {
  value = [for rg in azurerm_resource_group.test : format("$HOME/.kube/%s", rg.name)]
}