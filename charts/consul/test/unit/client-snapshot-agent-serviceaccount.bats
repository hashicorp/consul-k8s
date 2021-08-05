#!/usr/bin/env bats

load _helpers

@test "client/SnapshotAgentServiceAccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template -s templates/client-snapshot-agent-serviceaccount.yaml .
}

@test "client/SnapshotAgentServiceAccount: enabled with client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-serviceaccount.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentServiceAccount: enabled with client.enabled=true and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-serviceaccount.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentServiceAccount: disabled with client=false and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-serviceaccount.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=false' \
      .
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "client/SnapshotAgentServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-serviceaccount.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.imagePullSecrets[0].name=my-secret' \
      --set 'global.imagePullSecrets[1].name=my-secret2' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}

#--------------------------------------------------------------------
# client.snapshotAgent.serviceAccount.annotations

@test "client/SnapshotAgentServiceAccount: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-serviceaccount.yaml \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentServiceAccount: annotations when enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-serviceaccount.yaml \
      --set 'client.snapshotAgent.enabled=true' \
      --set "client.snapshotAgent.serviceAccount.annotations=foo: bar" \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
