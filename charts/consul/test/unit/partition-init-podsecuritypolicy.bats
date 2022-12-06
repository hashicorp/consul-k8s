#!/usr/bin/env bats

load _helpers

@test "partitionInit/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      .
}

@test "partitionInit/PodSecurityPolicy: enabled with global.adminPartitions.enabled=true and global.enablePodSecurityPolicies=true and server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "partitionInit/PodSecurityPolicy: disabled with global.adminPartitions.enabled=true and global.enablePodSecurityPolicies=false and server.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      --set 'server.enabled=false' \
      .
}

@test "partitionInit/PodSecurityPolicy: disabled with global.adminPartitions.enabled=true and global.enablePodSecurityPolicies=true and servers = true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'server.enabled=true' \
      .
}

@test "partitionInit/PodSecurityPolicy: disabled with global.adminPartitions.enabled=true and global.enablePodSecurityPolicies=false and servers = true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      --set 'server.enabled=true' \
      .
}

@test "partitionInit/PodSecurityPolicy: disabled with global.adminPartitions.enabled=true and global.enablePodSecurityPolicies=true and global.enabled = true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'global.enabled=true' \
      .
}

@test "partitionInit/PodSecurityPolicy: disabled with global.adminPartitions.enabled=true and global.enablePodSecurityPolicies=false and global.enabled = true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      --set 'global.enabled=true' \
      .
}

@test "partitionInit/PodSecurityPolicy: disabled with global.adminPartitions.enabled=false and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'server.enabled=true' \
      .
}

@test "partitionInit/PodSecurityPolicy: disabled with global.adminPartitions.enabled=false and global.enablePodSecurityPolicies=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-podsecuritypolicy.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      --set 'server.enabled=true' \
      .
}
