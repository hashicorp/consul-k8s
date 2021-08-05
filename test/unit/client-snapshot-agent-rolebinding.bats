#!/usr/bin/env bats

load _helpers

@test "client/SnapshotAgentRoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-rolebinding.yaml  \
      .
}

@test "client/SnapshotAgentRoleBinding: enabled with client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-rolebinding.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentRoleBinding: enabled with client.enabled=true and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-rolebinding.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentRoleBinding: disabled with client=false and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-rolebinding.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=false' \
      .
}
