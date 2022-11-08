#!/usr/bin/env bats

load _helpers

@test "expose-servers/Service: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/expose-servers-service.yaml \
      . 
}

@test "expose-servers/Service: enable with global.adminPartitions.enabled true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}


@test "expose-servers/Service: disable with server.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'server.enabled=false' \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      .
}

@test "expose-servers/Service: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      .
}

@test "expose-servers/Service: http port exists when tls is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.ports[0]' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("http"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "expose-servers/Service: only https port exists when tls is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.ports[0]' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("https"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "expose-servers/Service: http and https ports exist when tls is enabled and httpsOnly is false" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      . | tee /dev/stderr |
      yq '.spec.ports[1]' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("https"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local cmd=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      . | tee /dev/stderr |
      yq '.spec.ports[0]' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("http"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# annotations

@test "expose-servers/Service: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "expose-servers/Service: can set annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.exposeService.annotations=key: value' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# nodePort

@test "expose-servers/Service: HTTP node port can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.exposeService.type=NodePort' \
      --set 'server.exposeService.nodePort.http=4443' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "http") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4443" ]
}

@test "expose-servers/Service: HTTPS node port can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.exposeService.type=NodePort' \
      --set 'server.exposeService.nodePort.https=4443' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "https") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4443" ]
}

@test "expose-servers/Service: RPC node port can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.exposeService.type=NodePort' \
      --set 'server.exposeService.nodePort.rpc=4443' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "rpc") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4443" ]
}

@test "expose-servers/Service: Serf node port can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.exposeService.type=NodePort' \
      --set 'server.exposeService.nodePort.serf=4444' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "serflan") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4444" ]
}

@test "expose-servers/Service: Grpc node port can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.exposeService.type=NodePort' \
      --set 'server.exposeService.nodePort.grpc=4444' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[] | select(.name == "grpc") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4444" ]
}

@test "expose-servers/Service: RPC, Serf and grpc node ports can be set" {
  cd `chart_dir`
  local ports=$(helm template \
      -s templates/expose-servers-service.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.exposeService.type=NodePort' \
      --set 'server.exposeService.nodePort.rpc=4443' \
      --set 'server.exposeService.nodePort.grpc=4444' \
      --set 'server.exposeService.nodePort.serf=4445' \
      . | tee /dev/stderr |
      yq -r '.spec.ports[]' | tee /dev/stderr)

  local actual
  actual=$(echo $ports | jq -r 'select(.name == "rpc") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4443" ]

  actual=$(echo $ports | jq -r 'select(.name == "grpc") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4444" ]

  actual=$(echo $ports | jq -r 'select(.name == "serflan") | .nodePort' | tee /dev/stderr)
  [ "${actual}" == "4445" ]
}
