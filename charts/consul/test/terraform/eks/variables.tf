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

# ----------------------------------------------------------------------------
# HC-COMPUTE-010 / SECVULN-44200 host baseline package
#
# The Wiz/Uptycs compliance scanner checks the node's host package database
# (`dpkg --list | grep hc-security-base`). The Uptycs EDR DaemonSet above
# covers the Kubernetes posture control but does NOT register the hc-security-base
# package in the host package DB. The default EKS-optimized node image is Amazon
# Linux, which cannot carry that Debian package. When enable_security_baseline is
# true the worker nodes are launched from a Canonical Ubuntu EKS-optimized AMI and
# hc-security-base is installed at boot from HashiCorp internal Artifactory,
# mirroring ami-builder/scripts/packer-install_meta_deb.sh.
# ----------------------------------------------------------------------------

variable "enable_security_baseline" {
  type        = bool
  default     = false
  description = "When true, run worker nodes on a Canonical Ubuntu EKS AMI and install hc-security-base at boot to satisfy HC-COMPUTE-010. Requires afy_user/afy_password. Validate in a single cluster before enabling in shared CI."
}

variable "ubuntu_eks_ami_owner" {
  default     = "099720109477"
  description = "AWS account id that owns the Ubuntu EKS AMIs (Canonical)."
}

variable "ubuntu_eks_ami_name_filter" {
  default     = "ubuntu-eks/k8s_%s/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"
  description = "Name filter for the Canonical Ubuntu EKS AMI. %s is replaced with kubernetes_version."
}

variable "afy_user" {
  type        = string
  default     = ""
  sensitive   = true
  description = "HashiCorp Artifactory username used to fetch hc-security-base. Provide via TF_VAR_afy_user from CI secrets; never commit."
}

variable "afy_password" {
  type        = string
  default     = ""
  sensitive   = true
  description = "HashiCorp Artifactory password/token used to fetch hc-security-base. Provide via TF_VAR_afy_password from CI secrets; never commit."
}
