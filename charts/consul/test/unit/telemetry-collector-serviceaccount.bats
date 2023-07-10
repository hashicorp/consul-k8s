#!/usr/bin/env bats

load _helpers

@test "telemetryCollector/ServiceAccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-serviceaccount.yaml  \
      .
}

@test "telemetryCollector/ServiceAccount: enabled with telemetryCollector, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-serviceaccount.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "telemetryCollector/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-serviceaccount.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.imagePullSecrets[0].name=my-secret' \
      --set 'global.imagePullSecrets[1].name=my-secret2' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}

#--------------------------------------------------------------------
# telemetryCollector.serviceAccount.annotations

@test "telemetryCollector/ServiceAccount: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-serviceaccount.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "telemetryCollector/ServiceAccount: annotations when enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-serviceaccount.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set "telemetryCollector.serviceAccount.annotations=foo: bar" \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}


#--------------------------------------------------------------------
# consul.name

@test "telemetryCollector/ServiceAccount: name is constant regardless of consul name" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-serviceaccount.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'consul.name=foobar' \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "consul-telemetry-collector" ]
}
