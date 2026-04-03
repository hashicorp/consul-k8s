# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

output "primary_cluster_name" {
  value = var.primary_cluster_name
}

output "secondary_cluster_name" {
  value = var.secondary_cluster_name
}

output "primary_vpc_id" {
  value = aws_vpc.primary.id
}

output "secondary_vpc_id" {
  value = aws_vpc.secondary.id
}

output "vpc_peering_connection_id" {
  value = aws_vpc_peering_connection.peer.id
}

output "primary_private_subnet_ids" {
  value = aws_subnet.primary_private[*].id
}

output "secondary_private_subnet_ids" {
  value = aws_subnet.secondary_private[*].id
}

output "worker_security_group_note" {
  value = "Terraform adds bidirectional worker security group ingress for 8300, 8301/tcp, 8301/udp, 8501, and 8502 after both ROSA clusters become ready."
}