#!/usr/bin/env bats

load _helpers

@test "client/SnapshotAgentPodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-podsecuritypolicy.yaml  \
      .
}

@test "client/SnapshotAgentPodSecurityPolicy: disabled with snapshot agent disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-podsecuritypolicy.yaml  \
      --set 'client.snapshotAgent.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "client/SnapshotAgentPodSecurityPolicy: enabled with snapshot agent enabled global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-podsecuritypolicy.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
