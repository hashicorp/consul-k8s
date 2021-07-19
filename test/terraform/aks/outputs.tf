output "kubeconfigs" {
  value = local_file.kubeconfigs.*.filename
}
