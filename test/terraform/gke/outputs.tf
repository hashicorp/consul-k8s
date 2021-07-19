output "cluster_ids" {
  value = google_container_cluster.cluster.*.id
}

output "cluster_names" {
  value = google_container_cluster.cluster.*.name
}

output "kubeconfigs" {
  value = [for cl in google_container_cluster.cluster : format("$HOME/.kube/%s", cl.name)]
}
