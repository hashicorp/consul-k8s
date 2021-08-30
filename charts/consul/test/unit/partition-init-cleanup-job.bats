#!/usr/bin/env bats

load _helpers

@test "partitionInitCleanup/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-cleanup-job.yaml  \
      .
}

@test "partitionInitCleanup/Job: enabled with global.adminPartitions.enabled=true and servers = false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-cleanup-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "partitionInitCleanup/Job: disabled with global.adminPartitions.enabled=true and servers = true" {
  cd `chart_dir`
 assert_empty helm template \
      -s templates/partition-init-cleanup-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'server.enabled=true' \
      .
}

@test "partitionInitCleanup/Job: disabled with global.adminPartitions.enabled=true and global.enabled = true" {
  cd `chart_dir`
 assert_empty helm template \
      -s templates/partition-init-cleanup-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enabled=true' \
      .
}

@test "partitionInitCleanup/Job: disabled with global.adminPartitions.enabled=false" {
  cd `chart_dir`
 assert_empty helm template \
      -s templates/partition-init-cleanup-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'server.enabled=true' \
      .
}