#!/usr/bin/env bats

load _helpers

@test "apiGateway/GatewayClassConfig: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      .
}

@test "apiGateway/GatewayClassConfig: enabled with apiGateway.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: deployment config disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq '.spec | has("deployment") | not' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: deployment config enabled with defaultInstances=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.managedGatewayClass.deployment.defaultInstances=3' \
      . | tee /dev/stderr |
      yq '.spec.deployment.defaultInstances == 3' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: deployment config enabled with maxInstances=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.managedGatewayClass.deployment.maxInstances=3' \
      . | tee /dev/stderr |
      yq '.spec.deployment.maxInstances == 3' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: deployment config enabled with minInstances=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.managedGatewayClass.deployment.minInstances=3' \
      . | tee /dev/stderr |
      yq '.spec.deployment.minInstances == 3' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# externalServers

@test "apiGateway/GatewayClassConfig: externalServers set and clients disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.enabled=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.consul.address == "external-consul.host"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: externalServers set and clients enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.enabled=true' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.consul | has("address") | not' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}