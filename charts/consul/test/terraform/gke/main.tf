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
  count         = var.cluster_count
  name          = "subnet-${random_string.cluster_prefix.result}-${count.index}"
  ip_cidr_range = cidrsubnet("10.0.0.0/8", 8, count.index)
  network       = google_compute_network.custom_network.name
}

resource "google_container_cluster" "cluster" {
  provider = google
  count    = var.cluster_count

  name               = "consul-k8s-${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}"
  project            = var.project
  initial_node_count = 3
  location           = var.zone

  network    = google_compute_network.custom_network.name
  subnetwork = google_compute_subnetwork.subnet[count.index].self_link

  min_master_version = data.google_container_engine_versions.main.latest_master_version
  node_version       = data.google_container_engine_versions.main.latest_master_version
  node_config {
    tags         = ["consul-k8s-${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}"]
    machine_type = "e2-standard-8"
  }

  resource_labels     = var.labels
  deletion_protection = false
}

resource "google_compute_firewall" "firewall-rules" {
  count       = var.cluster_count > 1 ? var.cluster_count : 0
  project     = var.project
  name        = format("firewall-%s-%d", substr(random_string.cluster_prefix.result, 0, 8), count.index)
  network     = google_compute_network.custom_network.name
  description = "Firewall rule for cluster ${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}."

  allow {
    protocol = "all"
  }

  source_ranges = [
    google_container_cluster.cluster[count.index == 0 ? 1 : 0].cluster_ipv4_cidr
  ]
}
