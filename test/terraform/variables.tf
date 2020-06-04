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
