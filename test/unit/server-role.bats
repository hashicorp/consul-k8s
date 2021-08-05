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

#--------------------------------------------------------------------
# global.openshift.enabled

@test "server/Role: allows securitycontextconstraints access with global.openshift.enabled=true and server.exposeGossipAndRPCPorts=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-role.yaml  \
      --set 'server.enabled=true' \
      --set 'global.openshift.enabled=true' \
      --set 'server.exposeGossipAndRPCPorts=true' \
      . | tee /dev/stderr |
      yq -r '.rules[] | select(.resources==["securitycontextconstraints"]) | .resourceNames[0]' | tee /dev/stderr)
  [ "${actual}" = "RELEASE-NAME-consul-server" ]
}

@test "server/Role: allows SCC and PSP access with global.openshift.enabled=true,server.exposeGossipAndRPCPorts=true and global.enablePodSecurityPolices=true" {
  cd `chart_dir`
  local rules=$(helm template \
      -s templates/server-role.yaml  \
      --set 'server.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'server.exposeGossipAndRPCPorts=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[]' | tee /dev/stderr)

  local scc_resource=$(echo $rules | jq -r '. | select(.resources==["securitycontextconstraints"]) | .resourceNames[0]')
  [ "${scc_resource}" = "RELEASE-NAME-consul-server" ]

  local psp_resource=$(echo $rules | jq -r '. | select(.resources==["podsecuritypolicies"]) | .resourceNames[0]')
  [ "${psp_resource}" = "RELEASE-NAME-consul-server" ]
}
