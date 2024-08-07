#!/usr/bin/env bats

load _helpers

@test "dns/Service: enabled by default due to inheriting from connectInject.transparentProxy.defaultEnabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dns/Service: enable with connectInject.transparentProxy.defaultEnabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml  \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dns/Service: disable with dns.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-service.yaml  \
      --set 'dns.enabled=false' \
      .
}

@test "dns/Service: disable with connectInject.transparentProxy.defaultEnabled false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-service.yaml  \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      .
}

@test "dns/Service: disable with dns.proxy.enabled set to true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-service.yaml  \
      --set 'dns.enabled=true' \
      --set 'dns.proxy.enabled=true' \
      .
}

#--------------------------------------------------------------------
# annotations

@test "dns/Service: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml  \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "dns/Service: can set annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml  \
      --set 'dns.enabled=true' \
      --set 'dns.annotations=key: value' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# clusterIP

@test "dns/Service: clusterIP not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml  \
      . | tee /dev/stderr |
      yq '.spec | .clusterIP? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dns/Service: specified clusterIP" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml  \
      --set 'dns.clusterIP=192.168.1.1' \
      . | tee /dev/stderr |
      yq '.spec | .clusterIP == "192.168.1.1"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# service type

@test "dns/Service: service type ClusterIP by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml \
      . | tee /dev/stderr |
      yq '.spec | .type == "ClusterIP"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dns/Service: add custom service type" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml \
      --set dns.type=LoadBalancer \
      . | tee /dev/stderr |
      yq '.spec | .type == "LoadBalancer"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# additionalSpec

@test "dns/Service: add additionalSpec" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-service.yaml \
      --set dns.additionalSpec="loadBalancerIP: 192.168.0.100" \
      . | tee /dev/stderr |
      yq '.spec | .loadBalancerIP == "192.168.0.100"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
