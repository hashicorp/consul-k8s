#!/usr/bin/env bats

load _helpers

@test "ui/Ingress: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Ingress: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      --set 'ui.enabled=true' \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ui/Ingress: disable with server.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'server.enabled=false' \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Ingress: disable with ui.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.enabled=false' \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Ingress: disable with ui.ingress.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Ingress: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'global.enabled=false' \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Ingress: disable with global.enabled and server.enabled on" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# hosts

@test "ui/Ingress: no hosts by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.rules' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Ingress: hosts can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'ui.ingress.hosts[0]=foo.com' \
      . | tee /dev/stderr |
      yq -r '.spec.rules[0].host' | tee /dev/stderr)
  [ "${actual}" = "foo.com" ]
}

#--------------------------------------------------------------------
# tls

@test "ui/Ingress: no tls by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.tls' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Ingress: tls can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'ui.ingress.tls[0].hosts[0]=foo.com' \
      . | tee /dev/stderr |
      yq -r '.spec.tls[0].hosts[0]' | tee /dev/stderr)
  [ "${actual}" = "foo.com" ]
}

@test "ui/Ingress: tls with secret name can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'ui.ingress.tls[0].hosts[0]=foo.com' \
      --set 'ui.ingress.tls[0].secretName=testsecret-tls' \
      . | tee /dev/stderr |
      yq -r '.spec.tls[0].secretName' | tee /dev/stderr)
  [ "${actual}" = "testsecret-tls" ]
}

#--------------------------------------------------------------------
# annotations

@test "ui/Ingress: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Ingress: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'ui.ingress.annotations.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
