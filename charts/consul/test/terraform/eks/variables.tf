# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

variable "region" {
  default     = "us-west-2"
  description = "AWS region"
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

variable "role_arn" {
  default     = ""
  description = "AWS role for the AWS provider to assume when running these templates."
}

variable "tags" {
  type        = map(any)
  default     = {}
  description = "Tags to attach to the created resources."
}

variable "kubernetes_version" {
  default     = "1.32"
  description = "Kubernetes version supported on EKS"
}

# ----------------------------------------------------------------------------
# HC-COMPUTE-010 / SECVULN-44200 security baseline
#
# The Uptycs EDR agent (k8sosquery DaemonSet + kubequery) is deployed to every
# acceptance cluster so the ephemeral worker nodes are covered by the security
# baseline, mirroring the AKS and GKE acceptance modules.
# ----------------------------------------------------------------------------

variable "uptycs_owner" {
  default     = "hc-team-consul-dg@ibm.com"
  description = "Owner email for Uptycs EDR agent tags."
}

variable "uptycs_enroll_secret" {
  default     = ""
  description = "Uptycs enroll secret containing customer identifiers."
  sensitive   = true
}

variable "uptycs_webhook_ca_bundle" {
  default     = ""
  description = "Base64-encoded CA bundle for kubequery webhook."
  sensitive   = true
}

variable "uptycs_webhook_tls_crt" {
  default     = ""
  description = "Base64-encoded TLS certificate for kubequery webhook server."
  sensitive   = true
}

variable "uptycs_webhook_tls_key" {
  default     = ""
  description = "Base64-encoded TLS private key for kubequery webhook server."
  sensitive   = true
}
