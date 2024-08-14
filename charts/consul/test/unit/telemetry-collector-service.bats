#!/usr/bin/env bats

load _helpers

@test "telemetryCollector/Service: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-service.yaml  \
      .
}

@test "telemetryCollector/Service: enabled by default with telemetryCollector, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-service.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Service: enabled with telemetryCollector.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-service.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# consul.name

@test "telemetryCollector/Service: name is constant regardless of consul name" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-service.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'consul.name=foobar' \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "consul-telemetry-collector" ]
}

#--------------------------------------------------------------------
# annotations

@test "telemetryCollector/Service: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-service.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "telemetryCollector/Service: can set annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-service.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'telemetryCollector.service.annotations=key: value' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# port

@test "telemetryCollector/Service: has default port" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-service.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[0].port' | tee /dev/stderr)
  [ "${actual}" = "9356" ]
}