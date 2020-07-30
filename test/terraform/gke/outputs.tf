output "cluster_ids" {
  value = google_container_cluster.cluster.*.id
}

output "cluster_names" {
  value = google_container_cluster.cluster.*.name
}

output "context_names" {
  value = [for cl in google_container_cluster.cluster : format("gke_%s_%s_%s", var.project, var.zone, cl.name) ]
}
