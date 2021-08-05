#!/usr/bin/env bats

load _helpers

@test "server/SecurityContextConstraints: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-securitycontextconstraints.yaml  \
      .
}

@test "server/SecurityContextConstraints: disabled with server disabled and global.openshift.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-securitycontextconstraints.yaml  \
      --set 'server.enabled=false' \
      --set 'global.openshift.enabled=true' \
      .
}

@test "server/SecurityContextConstraints: enabled with global.openshift.enabled=true and server.exposeGossipAndRPCPorts=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-securitycontextconstraints.yaml  \
      --set 'global.openshift.enabled=true' \
      --set 'server.exposeGossipAndRPCPorts=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
