# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

output "cluster_pair_details" {
  value = {
    for pair_key, pair in local.cluster_pairs : pair_key => {
      primary_cluster_name         = pair.primary_cluster_name
      secondary_cluster_name       = pair.secondary_cluster_name
      openshift_version            = pair.openshift_version
      primary_vpc_id               = aws_vpc.primary[pair_key].id
      secondary_vpc_id             = aws_vpc.secondary[pair_key].id
      vpc_peering_connection_id    = aws_vpc_peering_connection.peer[pair_key].id
      primary_private_subnet_ids   = local.primary_private_subnet_ids[pair_key]
      secondary_private_subnet_ids = local.secondary_private_subnet_ids[pair_key]
    }
  }
}

output "primary_cluster_name" {
  value = local.single_pair_key == null ? null : local.cluster_pairs[local.single_pair_key].primary_cluster_name
}

output "secondary_cluster_name" {
  value = local.single_pair_key == null ? null : local.cluster_pairs[local.single_pair_key].secondary_cluster_name
}

output "primary_vpc_id" {
  value = local.single_pair_key == null ? null : aws_vpc.primary[local.single_pair_key].id
}

output "secondary_vpc_id" {
  value = local.single_pair_key == null ? null : aws_vpc.secondary[local.single_pair_key].id
}

output "vpc_peering_connection_id" {
  value = local.single_pair_key == null ? null : aws_vpc_peering_connection.peer[local.single_pair_key].id
}

output "primary_private_subnet_ids" {
  value = local.single_pair_key == null ? null : local.primary_private_subnet_ids[local.single_pair_key]
}

output "secondary_private_subnet_ids" {
  value = local.single_pair_key == null ? null : local.secondary_private_subnet_ids[local.single_pair_key]
}

output "rosa_create_commands" {
  value = {
    for pair_key in local.pair_keys : pair_key => {
      primary   = local.primary_rosa_create_commands[pair_key]
      secondary = local.secondary_rosa_create_commands[pair_key]
    }
  }
}

output "primary_rosa_create_command" {
  value = local.single_pair_key == null ? null : local.primary_rosa_create_commands[local.single_pair_key]
}

output "secondary_rosa_create_command" {
  value = local.single_pair_key == null ? null : local.secondary_rosa_create_commands[local.single_pair_key]
}

output "worker_security_group_ingress_commands" {
  value = local.worker_security_group_ingress_commands
}

output "worker_security_group_ingress_command" {
  value = local.single_pair_key == null ? null : local.worker_security_group_ingress_commands[local.single_pair_key]
}

output "worker_security_group_note" {
  value = "Run the per-pair worker security group ingress command after both clusters in a pair become ready to allow bidirectional worker-node ingress for 8300, 8301/tcp, 8301/udp, 8501, and 8502."
}

output "rosa_cluster_bootstrap_scripts" {
  value = local.pair_bootstrap_scripts
}

output "rosa_cluster_bootstrap_script" {
  value = local.single_pair_key == null ? null : local.pair_bootstrap_scripts[local.single_pair_key]
}
