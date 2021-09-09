#!/usr/bin/env bats

load _helpers

@test "partition/Service: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-service.yaml  \
      .
}

@test "partition/Service: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "partition/Service: disable with adminPartitions.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=false' \
      .
}

@test "partition/Service: disable with server.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'server.enabled=false' \
      .
}

@test "partition/Service: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-service.yaml  \
      --set 'global.enabled=false' \
      .
}

#--------------------------------------------------------------------
# annotations

@test "partition/Service: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "partition/Service: can set annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.service.annotations=key: value' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# nodePort

@test "partition/Service: RPC node port can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.service.type=NodePort' \
      --set 'global.adminPartitions.service.nodePort.rpc=4443' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "server") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4443" ]
}

@test "partition/Service: Serf node port can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.service.type=NodePort' \
      --set 'global.adminPartitions.service.nodePort.serf=4444' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "serflan") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4444" ]
}

@test "partition/Service: HTTPS node port can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.service.type=NodePort' \
      --set 'global.adminPartitions.service.nodePort.https=4444' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "https") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4444" ]
}

@test "partition/Service: RPC, Serf and HTTPS node ports can be set" {
  cd `chart_dir`
  local ports=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.service.type=NodePort' \
      --set 'global.adminPartitions.service.nodePort.rpc=4443' \
      --set 'global.adminPartitions.service.nodePort.https=4444' \
      --set 'global.adminPartitions.service.nodePort.serf=4445' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[]' | tee /dev/stderr)

  local actual
  actual=$(echo $ports | jq -r 'select(.name == "server") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4443" ]

  actual=$(echo $ports | jq -r 'select(.name == "https") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4444" ]

  actual=$(echo $ports | jq -r 'select(.name == "serflan") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4445" ]
}
