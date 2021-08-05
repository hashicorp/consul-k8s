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
}

variable "labels" {
  type = map
  default = {}
  description = "Labels to attach to the created resources."
}
