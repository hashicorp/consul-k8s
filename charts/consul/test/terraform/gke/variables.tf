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

  // We currently cannot support more than 2 cluster
  // because setting up peering is more complicated if cluster count is
  // more than two.
  validation {
    condition     = var.cluster_count < 3 && var.cluster_count > 0
    error_message = "The cluster_count value must be 1 or 2."
  }
}

variable "labels" {
  type        = map
  default     = {}
  description = "Labels to attach to the created resources."
}
