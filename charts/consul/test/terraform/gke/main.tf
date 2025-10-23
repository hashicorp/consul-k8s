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

# -----------------------------
# Random Identifiers
# -----------------------------
resource "random_id" "suffix" {
  count       = var.cluster_count
  byte_length = 4
}

resource "random_string" "cluster_prefix" {
  length  = 8
  upper   = false
  special = false
}

# -----------------------------
# Get Available GKE Versions
# -----------------------------
data "google_container_engine_versions" "main" {
  location       = var.zone
  version_prefix = var.kubernetes_version_prefix
}

# -----------------------------
# Network and Subnet Logic
# -----------------------------
# Create a network only if create_network = true
resource "google_compute_network" "custom_network" {
  count                   = var.create_network ? 1 : 0
  name                    = "network-${random_string.cluster_prefix.result}"
  auto_create_subnetworks = false

  lifecycle {
    prevent_destroy = false
    ignore_changes  = [name]
  }
}

# Determine which network name to use (created or existing)
locals {
  active_network_name = var.create_network ? google_compute_network.custom_network[0].name : var.shared_network_name
}

# Create subnets only when new network is created
resource "google_compute_subnetwork" "subnet" {
  count         = var.create_network ? var.cluster_count : 0
  name          = "subnet-${random_string.cluster_prefix.result}-${count.index}"
  ip_cidr_range = cidrsubnet(var.shared_network_cidr, 8, count.index)
  region        = var.subnet_region
  network       = local.active_network_name
}

# -----------------------------
# GKE Cluster(s)
# -----------------------------
resource "google_container_cluster" "cluster" {
  count    = var.cluster_count
  provider = google

  name               = "consul-k8s-${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}"
  project            = var.project
  location           = var.zone
  initial_node_count = 3

  min_master_version = data.google_container_engine_versions.main.latest_master_version
  node_version       = data.google_container_engine_versions.main.latest_master_version

  network    = local.active_network_name
  subnetwork = var.create_network ? google_compute_subnetwork.subnet[count.index].self_link : var.subnet

  node_config {
    machine_type = "e2-standard-8"
    tags         = ["consul-k8s-${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}"]
  }

  resource_labels     = var.labels
  deletion_protection = false
}

# -----------------------------
# Firewall Rules (only when new network created)
# -----------------------------
resource "google_compute_firewall" "firewall_rules" {
  count       = var.create_network && var.cluster_count > 1 ? var.cluster_count : 0
  project     = var.project
  name        = format("firewall-%s-%d", substr(random_string.cluster_prefix.result, 0, 8), count.index)
  network     = local.active_network_name
  description = "Firewall rule for cluster ${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}."

  allow {
    protocol = "all"
  }

  source_ranges = [google_container_cluster.cluster[count.index == 0 ? 1 : 0].cluster_ipv4_cidr]
  source_tags   = ["cluster-${random_string.cluster_prefix.result}-${count.index == 0 ? 1 : 0}"]
  target_tags   = ["cluster-${random_string.cluster_prefix.result}-${count.index}"]
}

# -----------------------------
# Optional kubectl Initialization
# -----------------------------
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
