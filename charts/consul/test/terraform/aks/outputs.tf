# Copyright IBM Corp. 2018, 2026
# SPDX-License-Identifier: MPL-2.0

output "kubeconfigs" {
  value = local_file.kubeconfigs.*.filename
}