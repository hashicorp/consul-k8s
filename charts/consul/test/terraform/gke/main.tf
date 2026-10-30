# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    google = {
      version = "~> 5.3.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12.0"
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
  name          = "subnet-${random_string.cluster_prefix.result}-${count.index}" // Ensure valid name
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
  min_master_version = data.google_container_engine_versions.main.latest_master_version
  node_version       = data.google_container_engine_versions.main.latest_master_version
  network            = google_compute_network.custom_network.name
  node_config {
    tags         = ["consul-k8s-${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}"]
    machine_type = "e2-standard-8"
  }
  subnetwork = google_compute_subnetwork.subnet[count.index].self_link
  ip_allocation_policy {
    cluster_ipv4_cidr_block = cidrsubnet("10.100.0.0/14", 2, count.index)
  }
  resource_labels     = var.labels
  deletion_protection = false
}


resource "google_compute_firewall" "firewall-rules" {
  project     = var.project
  name        = format("firewall-%s-%d", substr(random_string.cluster_prefix.result, 0, 8), count.index)
  network     = google_compute_network.custom_network.name
  description = "Firewall rule for cluster ${random_string.cluster_prefix.result}-${random_id.suffix[count.index].dec}."

  count = var.cluster_count > 1 ? var.cluster_count : 0

  allow {
    protocol = "all"
  }

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

# Deploy IBM Uptycs EDR agent to each GKE cluster.
data "google_client_config" "default" {}

provider "helm" {
  alias = "cluster_0"
  kubernetes {
    host                   = "https://${google_container_cluster.cluster[0].endpoint}"
    token                  = data.google_client_config.default.access_token
    cluster_ca_certificate = base64decode(google_container_cluster.cluster[0].master_auth[0].cluster_ca_certificate)
  }
}

provider "helm" {
  alias = "cluster_1"
  kubernetes {
    host                   = var.cluster_count > 1 ? "https://${google_container_cluster.cluster[1].endpoint}" : "https://${google_container_cluster.cluster[0].endpoint}"
    token                  = data.google_client_config.default.access_token
    cluster_ca_certificate = base64decode(var.cluster_count > 1 ? google_container_cluster.cluster[1].master_auth[0].cluster_ca_certificate : google_container_cluster.cluster[0].master_auth[0].cluster_ca_certificate)
  }
}

# Cluster 0 EDR
resource "helm_release" "uptycs_0" {
  depends_on       = [null_resource.kubectl]
  provider         = helm.cluster_0
  name             = "k8sosquery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "k8sosquery"
  namespace        = "uptycs"
  create_namespace = true
  cleanup_on_fail  = true

  values = [
    templatefile("${path.module}/k8sosquery-values.yaml", {
      enroll_secret = var.uptycs_enroll_secret
    })
  ]
}

resource "helm_release" "kubequery_0" {
  depends_on       = [helm_release.uptycs_0]
  provider         = helm.cluster_0
  name             = "kubequery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "kubequery"
  namespace        = "kubequery"
  create_namespace = true
  cleanup_on_fail  = true

  set {
    name  = "deployment.spec.hostname"
    value = google_container_cluster.cluster[0].name
  }

  values = [
    templatefile("${path.module}/kubequery-values.yaml", {
      enroll_secret     = var.uptycs_enroll_secret
      webhook_ca_bundle = var.uptycs_webhook_ca_bundle
      webhook_tls_crt   = var.uptycs_webhook_tls_crt
      webhook_tls_key   = var.uptycs_webhook_tls_key
    })
  ]
}

# Cluster 1 EDR (only when cluster_count > 1)
resource "helm_release" "uptycs_1" {
  count            = var.cluster_count > 1 ? 1 : 0
  depends_on       = [null_resource.kubectl]
  provider         = helm.cluster_1
  name             = "k8sosquery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "k8sosquery"
  namespace        = "uptycs"
  create_namespace = true
  cleanup_on_fail  = true

  values = [
    templatefile("${path.module}/k8sosquery-values.yaml", {
      enroll_secret = var.uptycs_enroll_secret
    })
  ]
}

resource "helm_release" "kubequery_1" {
  count            = var.cluster_count > 1 ? 1 : 0
  depends_on       = [helm_release.uptycs_1]
  provider         = helm.cluster_1
  name             = "kubequery"
  repository       = "https://uptycslabs.github.io/kspm-helm-charts"
  chart            = "kubequery"
  namespace        = "kubequery"
  create_namespace = true
  cleanup_on_fail  = true

  set {
    name  = "deployment.spec.hostname"
    value = google_container_cluster.cluster[1].name
  }

  values = [
    templatefile("${path.module}/kubequery-values.yaml", {
      enroll_secret     = var.uptycs_enroll_secret
      webhook_ca_bundle = var.uptycs_webhook_ca_bundle
      webhook_tls_crt   = var.uptycs_webhook_tls_crt
      webhook_tls_key   = var.uptycs_webhook_tls_key
    })
  ]
}
