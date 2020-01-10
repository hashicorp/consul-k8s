#!/usr/bin/env bats

load _helpers

@test "tlsInit/ClusterRole: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/ClusterRole: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-clusterrole.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/ClusterRole: disabled when server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-clusterrole.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/ClusterRole: enabled when global.tls.enabled=true and server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-clusterrole.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/ClusterRole: enabled with global.tls.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-clusterrole.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/ClusterRole: adds pod security polices with global.tls.enabled and global.enablePodSecurityPolicies" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-clusterrole.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[] | select(.resources==["podsecuritypolicies"]) | .resourceNames[0]' | tee /dev/stderr)

  [ "${actual}" = "release-name-consul-tls-init" ]
}
