#!/usr/bin/env bats

load _helpers

@test "createFederationSecret/RoleBinding: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-rolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "createFederationSecret/RoleBinding: enabled with global.createFederationSecret=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-rolebinding.yaml  \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
