#!/usr/bin/env bats

load _helpers

@test "server/Role: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-role.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/Role: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-role.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "server/Role: can be enabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-role.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/Role: disabled with server.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-role.yaml  \
      --set 'server.enabled=false' \
      .
}

@test "server/Role: enabled with server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-role.yaml  \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

# The rules key must always be set (#178).
@test "server/Role: rules empty with server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-role.yaml  \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq '.rules' | tee /dev/stderr)
  [ "${actual}" = "[]" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "server/Role: podsecuritypolicies are added when global.enablePodSecurityPolicies is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-role.yaml  \
      --set 'server.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
