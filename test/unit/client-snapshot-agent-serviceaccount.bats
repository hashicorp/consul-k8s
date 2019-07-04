#!/usr/bin/env bats

load _helpers

@test "client/SnapshotAgentServiceAccount: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentServiceAccount: enabled with client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-serviceaccount.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentServiceAccount: enabled with client.enabled=true and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-serviceaccount.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentServiceAccount: disabled with client=false and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-serviceaccount.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
