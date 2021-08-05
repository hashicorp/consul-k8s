output "kubeconfigs" {
  value = [for cl in module.eks : pathexpand(format("~/.kube/%s", cl.cluster_id))]
}
