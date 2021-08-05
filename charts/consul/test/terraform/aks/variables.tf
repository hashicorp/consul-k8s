variable "location" {
  default     = "West US 2"
  description = "The location to launch this AKS cluster in."
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
}

variable "tags" {
  type = map
  default = {}
  description = "Tags to attach to the created resources."
}
