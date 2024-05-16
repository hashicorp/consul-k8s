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

@test "apiGateway/GatewayClassConfig: imageEnvoy can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.imageEnvoy=bar' \
      . | tee /dev/stderr |
      yq '.spec.image.envoy' | tee /dev/stderr)
  [ "${actual}" = "\"bar\"" ]
}

#--------------------------------------------------------------------
# Consul server address

@test "apiGateway/GatewayClassConfig: Consul server address set with external servers and no clients." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'server.enabled=false' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.consul.address == "external-consul.host"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: Consul server address set with external servers and clients." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'server.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.consul.address == "$(HOST_IP)"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: Consul server address set with local servers and no clients." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.consul.address == "release-name-consul-server.default.svc"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: Consul server address set with local servers and clients." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.consul.address == "$(HOST_IP)"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# externalServers ports

@test "apiGateway/GatewayClassConfig: ports for externalServers when not using TLS." {
  cd `chart_dir`
  local ports=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.grpcPort=1234' \
      --set 'externalServers.httpsPort=5678' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.consul.ports' | tee /dev/stderr)

  local actual
  actual=$(echo $ports | jq -r '.grpc' | tee /dev/stderr)
  [ "${actual}" = "1234" ]

  actual=$(echo $ports | jq -r '.http' | tee /dev/stderr)
  [ "${actual}" = "5678" ]
}

@test "apiGateway/GatewayClassConfig: ports for externalServers when using TLS." {
  cd `chart_dir`
  local ports=$(helm template \
      -s templates/api-gateway-gatewayclassconfig.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.grpcPort=1234' \
      --set 'externalServers.httpsPort=5678' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.consul.ports' | tee /dev/stderr)

  local actual
  actual=$(echo $ports | jq -r '.grpc' | tee /dev/stderr)
  [ "${actual}" = "1234" ]

  actual=$(echo $ports | jq -r '.http' | tee /dev/stderr)
  [ "${actual}" = "5678" ]
}
