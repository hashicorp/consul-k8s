# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

output "cluster_ids" {
  value = google_container_cluster.cluster.*.id
}

output "cluster_names" {
  value = google_container_cluster.cluster.*.name
}

output "kubeconfigs" {
  value = [for cl in google_container_cluster.cluster : format("$HOME/.kube/%s", cl.name)]
}

output "versions" {
  value = data.google_container_engine_versions.main
}
