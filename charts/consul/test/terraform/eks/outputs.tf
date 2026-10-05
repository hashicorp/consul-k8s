# Copyright IBM Corp. 2018, 2026
# SPDX-License-Identifier: MPL-2.0

output "kubeconfigs" {
  value = [for cl in module.eks : pathexpand(format("~/.kube/%s", cl.cluster_id))]
}
