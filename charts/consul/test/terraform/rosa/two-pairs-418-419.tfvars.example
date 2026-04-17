primary_region   = "us-east-2"
secondary_region = "us-west-2"

cluster_pairs = {
  rosa418 = {
    primary_cluster_name   = "test-bed-418-east"
    secondary_cluster_name = "test-bed-418-west"
    openshift_version      = "4.18.36"
    worker_instance_type   = "m5.xlarge"
    worker_replicas        = 3
    az_count               = 1
    host_prefix            = 23

    primary_vpc_cidr       = "10.10.0.0/16"
    secondary_vpc_cidr     = "10.20.0.0/16"
    primary_service_cidr   = "172.30.0.0/16"
    secondary_service_cidr = "172.31.0.0/16"
    primary_pod_cidr       = "10.128.0.0/14"
    secondary_pod_cidr     = "10.132.0.0/14"

    primary_additional_rosa_args   = ["--channel-group", "stable"]
    secondary_additional_rosa_args = ["--channel-group", "stable"]

    tags = {
      owner   = "consul-k8s"
      purpose = "rosa-peered-testbed-418"
    }
  }

  rosa419 = {
    primary_cluster_name   = "test-bed-419-east"
    secondary_cluster_name = "test-bed-419-west"
    openshift_version      = "4.19.27"
    worker_instance_type   = "m5.xlarge"
    worker_replicas        = 3
    az_count               = 1
    host_prefix            = 23

    primary_vpc_cidr       = "10.30.0.0/16"
    secondary_vpc_cidr     = "10.40.0.0/16"
    primary_service_cidr   = "172.32.0.0/16"
    secondary_service_cidr = "172.33.0.0/16"
    primary_pod_cidr       = "10.136.0.0/14"
    secondary_pod_cidr     = "10.140.0.0/14"

    primary_additional_rosa_args   = ["--channel-group", "stable"]
    secondary_additional_rosa_args = ["--channel-group", "stable"]

    tags = {
      owner   = "consul-k8s"
      purpose = "rosa-peered-testbed-419"
    }
  }
}

# These CIDRs are intentionally disjoint so both pairs remain independent.
# Adjust them only if they overlap with existing routed networks in your AWS account.