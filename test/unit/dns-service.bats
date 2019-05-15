#!/usr/bin/env bats

load _helpers

@test "dns/Service: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/dns-service.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dns/Service: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/dns-service.yaml  \
      --set 'global.enabled=false' \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dns/Service: disable with dns.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/dns-service.yaml  \
      --set 'dns.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "dns/Service: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/dns-service.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# annotations

@test "dns/Service: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/dns-service.yaml  \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "dns/Service: can set annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/dns-service.yaml  \
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
      -x templates/dns-service.yaml  \
      . | tee /dev/stderr |
      yq '.spec | .clusterIP? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dns/Service: specified clusterIP" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/dns-service.yaml  \
      --set 'dns.clusterIP=192.168.1.1' \
      . | tee /dev/stderr |
      yq '.spec | .clusterIP == "192.168.1.1"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
