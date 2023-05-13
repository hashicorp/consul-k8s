#!/usr/bin/env bats

load _helpers

@test "telemetryCollector/ConfigMap: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-configmap.yaml  \
      .
}

@test "telemetryCollector/ConfigMap: enabled with telemetryCollector enabled and config has content" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-configmap.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.customExporterConfig="{}"' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/ConfigMap: disabled with telemetryCollector enabled and config is empty content" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-configmap.yaml  \
      --set 'telemetryCollector.enabled=true' \
      .
}