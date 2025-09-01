variable "openshift_version" {
  type        = string
  default     = "4.16.9"
  description = "The required version of Red Hat OpenShift for the cluster, for example '4.1.0'. If version is greater than the currently running version, an upgrade will be scheduled."
  validation {
    condition     = can(regex("^[0-9]*[0-9]+.[0-9]*[0-9]+.[0-9]*[0-9]+$", var.openshift_version))
    error_message = "openshift_version must be with structure <major>.<minor>.<patch> (for example 4.13.6)."
  }
}

variable "cluster_name" {
  type        = string
  description = "Name of the cluster. After the creation of the resource, it is not possible to update the attribute value."
}

variable "create_oidc" {
  type        = bool
  description = "Create an OIDC provider and config for the cluster. If false, you must provide an existing OIDC config ID to the rosa module."
  default     = false
}

variable "region" {
  type        = string
  description = "Region to create the resources in"
  default     = "us-east-1"
}

variable "operator_role_prefix" {
  type        = string
    description = "Prefix to use for the operator IAM roles created"
    default     = "c20-operator-role"
}
variable "oidc_config_id" {
  type        = string
  description = "The ID of the OIDC config to be associated with the cluster. Required if create_oidc is false."
  default     = "2j7dm2lchqq6mvchteg0gc1tmht0pn4k"
  
}

variable "oidc_endpoint_url" {
  type        = string
  description = "The URL of the OIDC provider to be associated with the cluster. Required if create_oidc is false."
  default     = "oidc.op1.openshiftapps.com/2j7dm2lchqq6mvchteg0gc1tmht0pn4k"
}
