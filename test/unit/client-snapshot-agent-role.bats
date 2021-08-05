#!/usr/bin/env bats

load _helpers

@test "client/SnapshotAgentRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-role.yaml  \
      .
}

@test "client/SnapshotAgentRole: enabled with client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-role.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentRole: enabled with client.enabled=true and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-role.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentRole: disabled with client=false and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-role.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=false' \
      .
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "client/SnapshotAgentRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-role.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "client/SnapshotAgentRole: allows secret access with global.bootsrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-role.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

@test "client/SnapshotAgentRole: allows secret access with global.bootsrapACLs=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-role.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[1].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}
