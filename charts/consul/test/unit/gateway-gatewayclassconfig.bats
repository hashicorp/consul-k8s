#!/usr/bin/env bats

load _helpers

@test "apiGateway/GatewayClassConfig: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gateway-gatewayclassconfig.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: disabled with connectInject.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gateway-gatewayclassconfig.yaml  \
      --set 'connectInject.enabled=false' \
      .
}

#--------------------------------------------------------------------
# fallback configuration
# to be removed in 1.17 (t-eckert 2023-05-23)

@test "apiGateway/GatewayClassConfig: fallback configuration is used when apiGateway.enabled is true" {
  cd `chart_dir`
  local spec=$(helm template \
      -s templates/gateway-gatewayclassconfig.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=testing' \
      --set 'apiGateway.managedGatewayClass.nodeSelector=foo: bar' \
      --set 'apiGateway.managedGatewayClass.tolerations=- key: bar' \
      --set 'apiGateway.managedGatewayClass.copyAnnotations.service.annotations=- bingo' \
      --set 'apiGateway.managedGatewayClass.serviceType=LoadBalancer' \
      . | tee /dev/stderr |
      yq '.spec' | tee /dev/stderr)

  local actual=$(echo "$spec" |
    jq -r '.nodeSelector.foo')
  [ "${actual}" = "bar" ]

  local actual=$(echo "$spec" |
    jq -r '.tolerations[0].key')
  [ "${actual}" = "bar" ]

  local actual=$(echo "$spec" |
    jq -r '.copyAnnotations.service[0]')
  [ "${actual}" = "bingo" ]

  local actual=$(echo "$spec" |
    jq -r '.serviceType')
  [ "${actual}" = "LoadBalancer" ]
}

#--------------------------------------------------------------------
# configuration

@test "apiGateway/GatewayClassConfig: default configuration" {
  cd `chart_dir`
  local spec=$(helm template \
      -s templates/gateway-gatewayclassconfig.yaml  \
      . | tee /dev/stderr |
      yq '.spec' | tee /dev/stderr)

  local actual=$(echo "$spec" |
    jq -r '.deployment.defaultInstances')
  [ "${actual}" = 1 ]

  local actual=$(echo "$spec" |
    jq -r '.deployment.maxInstances')
  [ "${actual}" = 1 ]

  local actual=$(echo "$spec" |
    jq -r '.deployment.minInstances')
  [ "${actual}" = 1 ]
}

@test "apigateway/gatewayclassconfig: custom configuration" {
  cd `chart_dir`
  local spec=$(helm template \
      -s templates/gateway-gatewayclassconfig.yaml  \
      --set 'connectInject.apiGateway.managedGatewayClass.nodeSelector=foo: bar' \
      --set 'connectInject.apiGateway.managedGatewayClass.tolerations=- key: bar' \
      --set 'connectInject.apiGateway.managedGatewayClass.copyAnnotations.service.annotations=- bingo' \
      --set 'connectInject.apiGateway.managedGatewayClass.serviceType=LoadBalancer' \
      . | tee /dev/stderr |
      yq '.spec' | tee /dev/stderr)

  local actual=$(echo "$spec" |
    jq -r '.deployment.defaultInstances')
  [ "${actual}" = "1" ]

  local actual=$(echo "$spec" |
    jq -r '.deployment.maxInstances')
  [ "${actual}" = "1" ]

  local actual=$(echo "$spec" |
    jq -r '.deployment.minInstances')
  [ "${actual}" = "1" ]

  local actual=$(echo "$spec" |
    jq -r '.nodeSelector.foo')
  [ "${actual}" = "bar" ]

  local actual=$(echo "$spec" |
    jq -r '.tolerations[0].key')
  [ "${actual}" = "bar" ]

  local actual=$(echo "$spec" |
    jq -r '.copyAnnotations.service[0]')
  [ "${actual}" = "bingo" ]

  local actual=$(echo "$spec" |
    jq -r '.serviceType')
  [ "${actual}" = "LoadBalancer" ]
}
