#!/usr/bin/env bats

load _helpers

@test "tlsInit/RoleBinding: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-rolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/RoleBinding: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-rolebinding.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/RoleBinding: enabled with global.tls.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-rolebinding.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/RoleBinding: disabled when server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-rolebinding.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/RoleBinding: enabled when global.tls.enabled=true and server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-rolebinding.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
