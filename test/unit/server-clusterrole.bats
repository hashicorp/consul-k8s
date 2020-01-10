#!/usr/bin/env bats

load _helpers

@test "server/ClusterRole: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ClusterRole: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrole.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ClusterRole: can be enabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ClusterRole: disabled with server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrole.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ClusterRole: enabled with server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrole.yaml  \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

# The rules key must always be set (#178).
@test "server/ClusterRole: rules empty with server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrole.yaml  \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq '.rules' | tee /dev/stderr)
  [ "${actual}" = "[]" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "server/ClusterRole: podsecuritypolicies are added when global.enablePodSecurityPolicies is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrole.yaml  \
      --set 'server.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
