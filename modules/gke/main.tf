provider "google" {
  project = "${var.project}"
}

resource "random_id" "suffix" {
  byte_length = 4
}

resource "google_container_cluster" "cluster" {
  name               = "consul-k8s-${random_id.suffix.dec}"
  project            = "${var.project}"
  enable_legacy_abac = true
  initial_node_count = 5
  zone               = "${var.zone}"
  min_master_version = "${var.k8s_version}"
  node_version       = "${var.k8s_version}"
}

resource "null_resource" "kubectl" {
  triggers {
    cluster = "${google_container_cluster.cluster.id}"
  }

  # On creation, we want to setup the kubectl credentials. The easiest way
  # to do this is to shell out to gcloud.
  provisioner "local-exec" {
    command = "gcloud container clusters get-credentials --zone=${var.zone} ${google_container_cluster.cluster.name}"
  }

  # On destroy we want to try to clean up the kubectl credentials. This
  # might fail if the credentials are already cleaned up or something so we
  # want this to continue on failure. Generally, this works just fine since
  # it only operates on local data.
  provisioner "local-exec" {
    when       = "destroy"
    on_failure = "continue"
    command    = "kubectl config get-clusters | grep ${google_container_cluster.cluster.name} | xargs -n1 kubectl config delete-cluster"
  }
}
