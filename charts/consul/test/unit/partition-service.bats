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

# This can be seen as testing just what we put into the YAML raw, but
# this is such an important part of making everything work we verify it here.
@test "partition/Service: tolerates unready endpoints" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations["service.alpha.kubernetes.io/tolerate-unready-endpoints"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.publishNotReadyAddresses' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "partition/Service: no HTTPS listener when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "https") | .port' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "partition/Service: HTTPS listener set when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "https") | .port' | tee /dev/stderr)
  [ "${actual}" == "8501" ]
}

@test "partition/Service: HTTP listener still active when httpsOnly is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "http") | .port' | tee /dev/stderr)
  [ "${actual}" == "8500" ]
}

@test "partition/Service: no HTTP listener when httpsOnly is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=true' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "http") | .port' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

#--------------------------------------------------------------------
# annotations

@test "partition/Service: one annotation by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
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
