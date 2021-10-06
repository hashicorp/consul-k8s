provider "google-beta" {
  project = var.project
  version = "~> 3.49.0"
}

resource "random_id" "suffix" {
  count       = var.cluster_count
  byte_length = 4
}

data "google_container_engine_versions" "main" {
  location       = var.zone
  version_prefix = "1.19."
}

resource "google_container_cluster" "cluster" {
  provider = "google-beta"

  count = var.cluster_count

  name               = "consul-k8s-${random_id.suffix[count.index].dec}"
  project            = var.project
  initial_node_count = 3
  location           = var.zone
  min_master_version = data.google_container_engine_versions.main.latest_master_version
  node_version       = data.google_container_engine_versions.main.latest_master_version

  pod_security_policy_config {
    enabled = true
  }

  resource_labels = var.labels
}

resource "null_resource" "kubectl" {
  count = var.init_cli ? var.cluster_count : 0

  triggers = {
    cluster = google_container_cluster.cluster[count.index].id
  }

  # On creation, we want to setup the kubectl credentials. The easiest way
  # to do this is to shell out to gcloud.
  provisioner "local-exec" {
    command = "KUBECONFIG=$HOME/.kube/${google_container_cluster.cluster[count.index].name} gcloud container clusters get-credentials --zone=${var.zone} ${google_container_cluster.cluster[count.index].name}"
  }

  # On destroy we want to try to clean up the kubectl credentials. This
  # might fail if the credentials are already cleaned up or something so we
  # want this to continue on failure. Generally, this works just fine since
  # it only operates on local data.
  provisioner "local-exec" {
    when       = destroy
    on_failure = continue
    command    = "rm $HOME/.kube/consul-k8s*"
  }
}
