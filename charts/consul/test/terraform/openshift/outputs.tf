output "kubeconfigs" {
  value = [for rg in azurerm_resource_group.test : format("$HOME/.kube/%s", rg.name)]
}