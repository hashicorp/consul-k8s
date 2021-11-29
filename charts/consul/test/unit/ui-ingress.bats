#!/usr/bin/env bats

load _helpers

@test "ui/Ingress: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/ui-ingress.yaml  \
      .
}

@test "ui/Ingress: enable with ui.ingress.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ui/Ingress: disable with ui.ingress.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=false' \
      .
}

@test "ui/Ingress: disable with ui.ingress.enabled dash string" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=-' \
      .
}

#--------------------------------------------------------------------
# hosts

@test "ui/Ingress: no hosts by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.rules' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Ingress: hosts can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'ui.ingress.hosts[0].host=foo.com' \
      . | tee /dev/stderr |
      yq -r '.spec.rules[0].host' | tee /dev/stderr)
  [ "${actual}" = "foo.com" ]
}

@test "ui/Ingress: exposes single port 80 when global.tls.enabled=false" {
# todo: test for Kube versions < 1.19 when helm supports --kube-version flag (https://github.com/helm/helm/pull/9040)
#  local actual=$(helm template \
#      -s templates/ui-ingress.yaml  \
#      --set 'ui.ingress.enabled=true' \
#      --set 'global.tls.enabled=false' \
#      --set 'ui.ingress.hosts[0].host=foo.com' \
#      --kube-version "1.18" \
#      . | tee /dev/stderr |
#      yq -r '.spec.rules[0].http.paths[0].backend.servicePort' | tee /dev/stderr)
  cd `chart_dir`
  local actual=$(helm template \
     -s templates/ui-ingress.yaml  \
     --set 'ui.ingress.enabled=true' \
     --set 'global.tls.enabled=false' \
     --set 'ui.ingress.hosts[0].host=foo.com' \
     . | tee /dev/stderr |
     yq -r '.spec.rules[0].http.paths[0].backend.service.port.number' | tee /dev/stderr)
  [ "${actual}" = "80" ]
}

@test "ui/Ingress: exposes single port 443 when global.tls.enabled=true and global.tls.httpsOnly=true" {
# todo: test for Kube versions < 1.19 when helm supports --kube-version flag (https://github.com/helm/helm/pull/9040)
#  local actual=$(helm template \
#      -s templates/ui-ingress.yaml  \
#      --set 'ui.ingress.enabled=true' \
#      --set 'global.tls.enabled=true' \
#      --set 'ui.ingress.hosts[0].host=foo.com' \
#      --kube-version "1.18" \
#      . | tee /dev/stderr |
#      yq -r '.spec.rules[0].http.paths[0].backend.servicePort' | tee /dev/stderr)
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'ui.ingress.hosts[0].host=foo.com' \
      . | tee /dev/stderr |
      yq -r '.spec.rules[0].http.paths[0].backend.service.port.number' | tee /dev/stderr)
  [ "${actual}" = "443" ]
}

@test "ui/Ingress: exposes the port 80 when global.tls.enabled=true and global.tls.httpsOnly=false" {
# todo: test for Kube versions < 1.19 when helm supports --kube-version flag (https://github.com/helm/helm/pull/9040)
#  local actual=$(helm template \
#      -s templates/ui-ingress.yaml  \
#      --set 'ui.ingress.enabled=true' \
#      --set 'global.tls.enabled=true' \
#      --set 'global.tls.httpsOnly=false' \
#      --set 'ui.ingress.hosts[0].host=foo.com' \
#      --kube-version "1.18" \
#      . | tee /dev/stderr |
#      yq -r '.spec.rules[0].http.paths[0].backend.servicePort' | tee /dev/stderr)
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      --set 'ui.ingress.hosts[0].host=foo.com' \
      . | tee /dev/stderr |
      yq -r '.spec.rules[0].http.paths[0].backend.service.port.number' | tee /dev/stderr)
  [ "${actual}" = "80" ]
}

@test "ui/Ingress: exposes the port 443 when global.tls.enabled=true and global.tls.httpsOnly=false" {
# todo: test for Kube versions < 1.19 when helm supports --kube-version flag (https://github.com/helm/helm/pull/9040)
#  local actual=$(helm template \
#      -s templates/ui-ingress.yaml  \
#      --set 'ui.ingress.enabled=true' \
#      --set 'global.tls.enabled=true' \
#      --set 'global.tls.httpsOnly=false' \
#      --set 'ui.ingress.hosts[0].host=foo.com' \
#      --kube-version "1.18" \
#      . | tee /dev/stderr |
#      yq -r '.spec.rules[0].http.paths[1].backend.servicePort' | tee /dev/stderr)
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      --set 'ui.ingress.hosts[0].host=foo.com' \
      . | tee /dev/stderr |
      yq -r '.spec.rules[0].http.paths[1].backend.service.port.number' | tee /dev/stderr)
  [ "${actual}" = "443" ]
}

#--------------------------------------------------------------------
# tls

@test "ui/Ingress: no tls by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.tls' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Ingress: tls can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'ui.ingress.tls[0].hosts[0]=foo.com' \
      . | tee /dev/stderr |
      yq -r '.spec.tls[0].hosts[0]' | tee /dev/stderr)
  [ "${actual}" = "foo.com" ]
}

@test "ui/Ingress: tls with secret name can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
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
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Ingress: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ui-ingress.yaml  \
      --set 'ui.ingress.enabled=true' \
      --set 'ui.ingress.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# pathtype

@test "ui/Ingress: default PathType Prefix" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/ui-ingress.yaml  \
    --set 'ui.ingress.enabled=true' \
    --set 'global.tls.enabled=false' \
    --set 'ui.ingress.hosts[0].host=foo.com' \
    . | tee /dev/stderr |
    yq -r '.spec.rules[0].http.paths[0].pathType' | tee /dev/stderr)
  [ "${actual}" = "Prefix" ]
}

@test "ui/Ingress: set PathType ImplementationSpecific" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/ui-ingress.yaml  \
    --set 'ui.ingress.enabled=true' \
    --set 'ui.ingress.pathType=ImplementationSpecific' \
    --set 'global.tls.enabled=false' \
    --set 'ui.ingress.hosts[0].host=foo.com' \
    . | tee /dev/stderr |
    yq -r '.spec.rules[0].http.paths[0].pathType' | tee /dev/stderr)
  [ "${actual}" = "ImplementationSpecific" ]
}
