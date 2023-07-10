# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    google = {
      version = "~> 4.58.0"
    }
  }
}

provider "google" {
  project = var.project
  zone    = var.zone
}

resource "random_id" "suffix" {
  count       = var.cluster_count
  byte_length = 4
}

data "google_container_engine_versions" "main" {
  location       = var.zone
  version_prefix = "1.25.9"
}

# We assume that the subnets are already created to save time.
data "google_compute_subnetwork" "subnet" {
  name = var.subnet
}

resource "google_container_cluster" "cluster" {
  provider = "google"
  count    = var.cluster_count

  name               = "consul-k8s-${random_id.suffix[count.index].dec}"
  project            = var.project
  initial_node_count = 3
  location           = var.zone
  min_master_version = data.google_container_engine_versions.main.latest_master_version
  node_version       = data.google_container_engine_versions.main.latest_master_version
  node_config {
    tags         = ["consul-k8s-${random_id.suffix[count.index].dec}"]
    machine_type = "e2-standard-8"
  }
  subnetwork      = data.google_compute_subnetwork.subnet.name
  resource_labels = var.labels
}

resource "google_compute_firewall" "firewall-rules" {
  project     = var.project
  name        = "consul-k8s-acceptance-firewall-${random_id.suffix[count.index].dec}"
  network     = "default"
  description = "Creates firewall rule allowing traffic from nodes and pods of the ${random_id.suffix[count.index == 0 ? 1 : 0].dec} Kubernetes cluster."

  count = var.cluster_count > 1 ? var.cluster_count : 0

  allow {
    protocol = "all"
  }

  source_ranges = [google_container_cluster.cluster[count.index == 0 ? 1 : 0].cluster_ipv4_cidr]
  source_tags   = ["consul-k8s-${random_id.suffix[count.index == 0 ? 1 : 0].dec}"]
  target_tags   = ["consul-k8s-${random_id.suffix[count.index].dec}"]
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
