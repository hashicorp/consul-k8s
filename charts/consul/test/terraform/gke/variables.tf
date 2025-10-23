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
  default     = "asia-south1-a"
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

#########################################
# Newly Added Variables for Optimized Network Usage
#########################################

variable "create_network" {
  type        = bool
  default     = false
  description = "Whether to create a new shared network if not existing. Set to true only for the first run."
}

variable "shared_network_name" {
  type        = string
  default     = ""
  description = "Name of an existing shared network to reuse. If set, Terraform will not create a new network."
}

variable "shared_network_cidr" {
  type        = string
  default     = "10.0.0.0/16"
  description = "Base CIDR range for creating subnets for each cluster when using a shared network."
}

variable "subnet_region" {
  type        = string
  default     = "asia-south1"
  description = "Region where subnets will be created inside the shared network."
}
