#!/usr/bin/env bats

load _helpers

@test "partitionInit/Role: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-role.yaml  \
      .
}

@test "partitionInit/Role: enabled with global.adminPartitions.enabled=true and server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-role.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
@test "partitionInit/Role: enabled with global.rbac.create false" {
  cd `chart_dir`
    assert_empty helm template \
        -s templates/partition-init-role.yaml  \
        --set 'global.adminPartitions.enabled=true' \
        --set 'global.enableConsulNamespaces=true' \
        --set 'server.enabled=true' \
        --set 'global.rbac.create=false'  \
        .
}
@test "partitionInit/Role: disabled with global.adminPartitions.enabled=true and server.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-role.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=true' \
      .
}

@test "partitionInit/Role: disabled with global.adminPartitions.enabled=true and global.enabled = true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-role.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enabled=true' \
      .
}

@test "partitionInit/Role: disabled with global.adminPartitions.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-role.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=true' \
      .
}
