# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

variable "location" {
  default     = "West US 2"
  description = "The location to launch this AKS cluster in."
}

variable "kubernetes_version" {
  default     = "1.28"
  description = "Kubernetes version supported on AKS"
}

variable "client_id" {
  default     = ""
  description = "The client ID of the service principal to be used by Kubernetes when creating Azure resources like load balancers."
}

variable "client_secret" {
  default     = ""
  description = "The client secret of the service principal to be used by Kubernetes when creating Azure resources like load balancers."
}

variable "cluster_count" {
  default     = 1
  description = "The number of Kubernetes clusters to create."

  // We currently cannot support more than 2 clusters
  // because setting up peering is more complicated if cluster count is
  // more than two.
  validation {
    condition     = var.cluster_count < 3 && var.cluster_count > 0
    error_message = "The cluster_count value must be 1 or 2."
  }
}

variable "tags" {
  type        = map(any)
  default     = {}
  description = "Tags to attach to the created resources."
}
