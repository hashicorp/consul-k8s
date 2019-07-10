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

@test "ui/Ingress: enable with ui.ingress.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
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

@test "ui/Ingress: disable with ui.ingress.enabled dash string" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=-' \
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
      --set 'ui.ingress.tls[0].hosts[0]=sslexample.foo.com' \
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
      --set 'ui.ingress.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
