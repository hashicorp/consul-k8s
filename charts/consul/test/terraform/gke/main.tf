# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
terraform {
  required_providers {
    google = {
      version = "~> 5.3.0"
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
resource "random_string" "cluster_prefix" {
  length  = 8
  upper   = false
  special = false
}
data "google_container_engine_versions" "main" {
  location       = var.zone
  version_prefix = var.kubernetes_version_prefix
}
resource "google_compute_network" "custom_network" {
  name                    = "network-${random_string.cluster_prefix.result}"
  auto_create_subnetworks = false
  lifecycle {
    prevent_destroy = false
    ignore_changes  = [name]
  }
}

resource "google_compute_subnetwork" "subnet" {
@@ -59,6 +63,9 @@ resource "google_container_cluster" "cluster" {
    machine_type = "e2-standard-8"
  }
  subnetwork          = google_compute_subnetwork.subnet[count.index].self_link
    ip_allocation_policy {
    cluster_ipv4_cidr_block = cidrsubnet("10.100.0.0/14", 2, count.index)
  }
  resource_labels     = var.labels
  deletion_protection = false
}
@@ -76,9 +83,11 @@ resource "google_compute_firewall" "firewall-rules" {
    protocol = "all"
  }

  source_ranges = [google_container_cluster.cluster[count.index == 0 ? 1 : 0].cluster_ipv4_cidr]
  source_tags   = ["cluster-${random_string.cluster_prefix.result}-${count.index == 0 ? 1 : 0}"]
  target_tags   = ["cluster-${random_string.cluster_prefix.result}-${count.index}"]
  source_ranges = [
    google_container_cluster.cluster[count.index == 0 ? 1 : 0].cluster_ipv4_cidr,
    google_compute_subnetwork.subnet[count.index == 0 ? 1 : 0].ip_cidr_range
  ]
  target_tags = ["consul-k8s-${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}"]
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