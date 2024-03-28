# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

variable "location" {
  default     = "westus2"
  description = "The Azure Region to create all resources in."
}

variable "cluster_count" {
  default     = 1
  description = "The number of OpenShift clusters to create."
}
