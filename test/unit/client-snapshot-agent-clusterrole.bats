#!/usr/bin/env bats

load _helpers

@test "client/SnapshotAgentClusterRole: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentClusterRole: enabled with client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-clusterrole.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentClusterRole: enabled with client.enabled=true and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-clusterrole.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentClusterRole: disabled with client=false and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-clusterrole.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "client/SnapshotAgentClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-clusterrole.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

#--------------------------------------------------------------------
# global.bootstrapACLs

@test "client/SnapshotAgentClusterRole: allows secret access with global.bootsrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-clusterrole.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

@test "client/SnapshotAgentClusterRole: allows secret access with global.bootsrapACLs=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-clusterrole.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[1].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}
