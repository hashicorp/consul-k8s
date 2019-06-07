#!/usr/bin/env bats

load _helpers

@test "client/SnapshotAgentDeployment: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: enabled with client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: enabled with client.enabled=true and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: disabled with client=false and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# tolerations

@test "client/SnapshotAgentDeployment: no tolerations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates tolerations when client.tolerations is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.tolerations=allow' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations | contains("allow")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "client/SnapshotAgentDeployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates priorityClassName when client.priorityClassName is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.priorityClassName=allow' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName | contains("allow")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.bootstrapACLs and snapshotAgent.configSecret

@test "client/SnapshotAgentDeployment: no initContainer by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/SnapshotAgentDeployment: populates initContainer when global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: no volumes by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/SnapshotAgentDeployment: populates volumes when global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: populates volumes when client.snapshotAgent.configSecret.secretName and client.snapshotAgent.configSecret secretKey are defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=secret' \
      --set 'client.snapshotAgent.configSecret.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: no container volumeMounts by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/SnapshotAgentDeployment: populates container volumeMounts when global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: populates container volumeMounts when client.snapshotAgent.configSecret.secretName and client.snapshotAgent.configSecret secretKey are defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=secret' \
      --set 'client.snapshotAgent.configSecret.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "client/SnapshotAgentDeployment: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates nodeSelector when client.nodeSelector is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.nodeSelector=allow' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector | contains("allow")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
