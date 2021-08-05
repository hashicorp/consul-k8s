variable "location" {
  default     = "westus2"
  description = "The Azure Region to create all resources in."
}

variable "cluster_count" {
  default     = 1
  description = "The number of OpenShift clusters to create."
}

variable "tags" {
  type        = map
  default     = {}
  description = "Tags to attach to the created resources."
}
