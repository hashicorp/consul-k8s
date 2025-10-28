# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

variable "project" {
  description = <<EOF
Google Cloud Project to launch resources in. This project must have GKE
enabled and billing activated. We can't use the GOOGLE_PROJECT environment
variable since we need to access the project for other uses.
EOF
}

variable "zone" {
  default     = "us-central1-a"
  description = "The zone to launch all the GKE nodes in."
}

variable "init_cli" {
  default     = false
  description = "Whether to init kubectl."
}

variable "cluster_count" {
  default     = 1
  description = "The number of Kubernetes clusters to create."

  validation {
    condition     = var.cluster_count < 3 && var.cluster_count > 0
    error_message = "The cluster_count value must be 1 or 2."
  }
}

variable "labels" {
  type        = map(any)
  default     = {}
  description = "Labels to attach to the created resources."
}

variable "subnet" {
  type        = string
  default     = "default"
  description = "Subnet to create the cluster in. Currently all clusters use the default subnet and we are running out of IPs"
}

variable "kubernetes_version_prefix" {
  default     = "1.32."
  description = "Kubernetes version supported on EKS"
}

variable "shared_network_name" {
  type        = string
  description = "Existing shared VPC network name"
}

variable "shared_subnet_name" {
  type        = string
  description = "Existing shared subnet name"
}