#!/usr/bin/env bats

load _helpers

@test "gossipEncryptionAutogeneration/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogeneration-podsecuritypolicy.yaml  \
      .
}

@test "gossipEncryptionAutogeneration/PodSecurityPolicy: disabled with global.gossipEncryption.autoGenerate=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogeneration-podsecuritypolicy.yaml  \
      --set 'global.gossipEncryption.autoGenerate=false' \
      .
}

@test "gossipEncryptionAutogeneration/PodSecurityPolicy: enabled with global.gossipEncryption.autoGenerate=test" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogeneration-podsecuritypolicy.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}