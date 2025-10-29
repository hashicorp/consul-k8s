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

# ---------------------------------------------------------------------------
# SHARED NETWORK & SUBNET SECTION
# ---------------------------------------------------------------------------

resource "google_compute_network" "shared_network" {
  name                    = "shared-network-${random_string.cluster_prefix.result}"
  auto_create_subnetworks = false

  lifecycle {
    prevent_destroy = false
    ignore_changes  = [name]
  }
}

resource "google_compute_subnetwork" "shared_subnet" {
  name          = "shared-subnet-${random_string.cluster_prefix.result}"
  ip_cidr_range = "10.0.0.0/16"
  network       = google_compute_network.shared_network.name
  region        = substr(var.zone, 0, length(var.zone) - 2)
}

# ---------------------------------------------------------------------------
# CLUSTER CREATION SECTION
# ---------------------------------------------------------------------------

resource "google_container_cluster" "cluster" {
  count    = var.cluster_count
  provider = google

  name               = "consul-k8s-${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}"
  project            = var.project
  initial_node_count = 3
  location           = var.zone
  min_master_version = data.google_container_engine_versions.main.latest_master_version
  node_version       = data.google_container_engine_versions.main.latest_master_version

  network    = google_compute_network.shared_network.self_link
  subnetwork = google_compute_subnetwork.shared_subnet.self_link

  node_config {
    tags         = ["shared-firewall-${random_string.cluster_prefix.result}"]
    machine_type = "e2-standard-8"
  }

  resource_labels     = var.labels
  deletion_protection = false
}

# ---------------------------------------------------------------------------
# SHARED FIREWALL RULES
# ---------------------------------------------------------------------------

resource "google_compute_firewall" "shared_firewall" {
  name        = "shared-firewall-${random_string.cluster_prefix.result}"
  project     = var.project
  network     = google_compute_network.shared_network.name
  description = "Shared firewall rules for all clusters"

  allow {
    protocol = "all"
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["shared-firewall-${random_string.cluster_prefix.result}"]
}

# ---------------------------------------------------------------------------
# KUBECTL CONFIG SECTION
# ---------------------------------------------------------------------------

resource "null_resource" "kubectl" {
  count = var.init_cli ? var.cluster_count : 0

  triggers = {
    cluster = google_container_cluster.cluster[count.index].id
  }

  provisioner "local-exec" {
    command = "KUBECONFIG=$HOME/.kube/${google_container_cluster.cluster[count.index].name} gcloud container clusters get-credentials --zone=${var.zone} ${google_container_cluster.cluster[count.index].name}"
  }

  provisioner "local-exec" {
    when       = destroy
    on_failure = continue
    command    = "rm $HOME/.kube/consul-k8s*"
  }
}
