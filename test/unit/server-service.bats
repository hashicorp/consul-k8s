#!/usr/bin/env bats

load _helpers

@test "server/Service: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/Service: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/Service: disable with server.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/Service: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

# This can be seen as testing just what we put into the YAML raw, but
# this is such an important part of making everything work we verify it here.
@test "server/Service: tolerates unready endpoints" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations["service.alpha.kubernetes.io/tolerate-unready-endpoints"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(helm template \
      -x templates/server-service.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.publishNotReadyAddresses' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "server/Service: no HTTPS listener when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "https") | .port' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "server/Service: HTTPS listener set when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "https") | .port' | tee /dev/stderr)
  [ "${actual}" == "8501" ]
}

@test "server/Service: HTTP listener still active when httpsOnly is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "http") | .port' | tee /dev/stderr)
  [ "${actual}" == "8500" ]
}

@test "server/Service: no HTTP listener when httpsOnly is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=true' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "http") | .port' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

#--------------------------------------------------------------------
# annotations

@test "server/Service: one annotation by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/Service: can set annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-service.yaml  \
      --set 'server.service.annotations=key: value' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}
