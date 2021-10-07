variable "region" {
  default     = "us-west-2"
  description = "AWS region"
}

variable "cluster_count" {
  default     = 1
  description = "The number of Kubernetes clusters to create."
}

variable "role_arn" {
  default     = ""
  description = "AWS role for the AWS provider to assume when running these templates."
}

variable "tags" {
  type        = map
  default     = {}
  description = "Tags to attach to the created resources."
}
