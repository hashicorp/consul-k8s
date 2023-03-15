# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

output "kubeconfigs" {
  value = local_file.kubeconfigs.*.filename
}