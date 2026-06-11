# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

variable "primary_region" {
  type        = string
  default     = "us-east-2"
  description = "AWS region for the primary ROSA cluster."
}

variable "secondary_region" {
  type        = string
  default     = "us-west-2"
  description = "AWS region for the secondary ROSA cluster."
}

variable "role_arn" {
  type        = string
  default     = ""
  description = "Optional AWS role for the providers to assume."
}

variable "primary_cluster_name" {
  type        = string
  default     = "consul-rosa-east"
  description = "Name of the primary ROSA cluster."
}

variable "secondary_cluster_name" {
  type        = string
  default     = "consul-rosa-west"
  description = "Name of the secondary ROSA cluster."
}

variable "openshift_version" {
  type        = string
  default     = "4.18.36"
  description = "OpenShift version passed to rosa create cluster."
}

variable "worker_instance_type" {
  type        = string
  default     = "m5.xlarge"
  description = "Worker instance type for both ROSA clusters."
}

variable "worker_replicas" {
  type        = number
  default     = 3
  description = "Number of workers per cluster."
}

variable "az_count" {
  type        = number
  default     = 1
  description = "Number of AZs and subnets to create per VPC."

  validation {
    condition     = var.az_count >= 1 && var.az_count <= 3
    error_message = "This module supports 1 to 3 AZs per ROSA cluster."
  }
}

variable "primary_vpc_cidr" {
  type        = string
  default     = "10.10.0.0/16"
  description = "CIDR block for the primary ROSA VPC."
}

variable "secondary_vpc_cidr" {
  type        = string
  default     = "10.20.0.0/16"
  description = "CIDR block for the secondary ROSA VPC."
}

variable "primary_service_cidr" {
  type        = string
  default     = "172.30.0.0/16"
  description = "Service CIDR for the primary ROSA cluster."
}

variable "secondary_service_cidr" {
  type        = string
  default     = "172.31.0.0/16"
  description = "Service CIDR for the secondary ROSA cluster."
}

variable "primary_pod_cidr" {
  type        = string
  default     = "10.128.0.0/14"
  description = "Pod CIDR for the primary ROSA cluster."
}

variable "secondary_pod_cidr" {
  type        = string
  default     = "10.132.0.0/14"
  description = "Pod CIDR for the secondary ROSA cluster."
}

variable "host_prefix" {
  type        = number
  default     = 23
  description = "Host prefix passed to rosa create cluster."
}

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Tags applied to created AWS resources."
}

variable "primary_additional_rosa_args" {
  type        = list(string)
  default     = []
  description = "Additional rosa create cluster flags for the primary cluster."
}

variable "secondary_additional_rosa_args" {
  type        = list(string)
  default     = []
  description = "Additional rosa create cluster flags for the secondary cluster."
}

variable "rosa_create_timeout" {
  type        = string
  default     = "120m"
  description = "Timeout used by terraform while waiting for rosa cluster creation commands to finish."
}

variable "cluster_pairs" {
  type = map(object({
    primary_cluster_name           = string
    secondary_cluster_name         = string
    primary_vpc_cidr               = string
    secondary_vpc_cidr             = string
    primary_service_cidr           = string
    secondary_service_cidr         = string
    primary_pod_cidr               = string
    secondary_pod_cidr             = string
    openshift_version              = optional(string)
    worker_instance_type           = optional(string)
    worker_replicas                = optional(number)
    az_count                       = optional(number)
    host_prefix                    = optional(number)
    primary_additional_rosa_args   = optional(list(string))
    secondary_additional_rosa_args = optional(list(string))
    tags                           = optional(map(string))
  }))
  default     = {}
  description = "Optional map of independent primary/secondary ROSA cluster pairs. When set, Terraform creates one fully isolated peered VPC pair per entry. Regions remain controlled by primary_region and secondary_region for all pairs."

  validation {
    condition = alltrue([
      for pair in values(var.cluster_pairs) : try(pair.az_count, 1) >= 1 && try(pair.az_count, 1) <= 3
    ])
    error_message = "Each cluster_pairs entry must use an az_count between 1 and 3."
  }

  validation {
    condition = length(distinct(flatten([
      for pair in values(var.cluster_pairs) : [
        pair.primary_cluster_name,
        pair.secondary_cluster_name,
      ]
      ]))) == length(flatten([
      for pair in values(var.cluster_pairs) : [
        pair.primary_cluster_name,
        pair.secondary_cluster_name,
      ]
    ]))
    error_message = "Each cluster name in cluster_pairs must be unique."
  }

  validation {
    condition = length(distinct(flatten([
      for pair in values(var.cluster_pairs) : [
        pair.primary_vpc_cidr,
        pair.secondary_vpc_cidr,
        pair.primary_service_cidr,
        pair.secondary_service_cidr,
        pair.primary_pod_cidr,
        pair.secondary_pod_cidr,
      ]
      ]))) == length(flatten([
      for pair in values(var.cluster_pairs) : [
        pair.primary_vpc_cidr,
        pair.secondary_vpc_cidr,
        pair.primary_service_cidr,
        pair.secondary_service_cidr,
        pair.primary_pod_cidr,
        pair.secondary_pod_cidr,
      ]
    ]))
    error_message = "cluster_pairs CIDR values must be unique across all pairs."
  }
}