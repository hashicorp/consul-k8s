#!/usr/bin/env bats

load _helpers

@test "meshGateway/Service: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/mesh-gateway-service.yaml  \
      .
}

@test "meshGateway/Service: enabled by default with meshGateway, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Service: enabled with meshGateway.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# annotations

@test "meshGateway/Service: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Service: can set annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.service.annotations=key: value' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# port

@test "meshGateway/Service: has default port" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[0].port' | tee /dev/stderr)
  [ "${actual}" = "443" ]
}

@test "meshGateway/Service: can set port" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.service.port=8443' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[0].port' | tee /dev/stderr)
  [ "${actual}" = "8443" ]
}

#--------------------------------------------------------------------
# targetPort

@test "meshGateway/Service: has default targetPort" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[0].targetPort' | tee /dev/stderr)
  [ "${actual}" = "8443" ]
}

@test "meshGateway/Service: uses targetPort from containerPort" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.containerPort=9443' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[0].targetPort' | tee /dev/stderr)
  [ "${actual}" = "9443" ]
}

#--------------------------------------------------------------------
# nodePort

@test "meshGateway/Service: no nodePort by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[0].nodePort' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Service: can set a nodePort" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.service.nodePort=8443' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[0].nodePort' | tee /dev/stderr)
  [ "${actual}" = "8443" ]
}

#--------------------------------------------------------------------
# Service type

@test "meshGateway/Service: defaults to type LoadBalancer" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.type' | tee /dev/stderr)
  [ "${actual}" = "LoadBalancer" ]
}

@test "meshGateway/Service: can set type" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.service.type=ClusterIP' \
      . | tee /dev/stderr |
      yq -r '.spec.type' | tee /dev/stderr)
  [ "${actual}" = "ClusterIP" ]
}

#--------------------------------------------------------------------
# additionalSpec

@test "meshGateway/Service: can add additionalSpec" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-service.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.service.additionalSpec=key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}
