#!/usr/bin/env bats

load _helpers

@test "gossipEncryptionAutogenerate/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogenerate-podsecuritypolicy.yaml  \
      .
}

@test "gossipEncryptionAutogenerate/PodSecurityPolicy: disabled with global.gossipEncryption.autoGenerate=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogenerate-podsecuritypolicy.yaml  \
      --set 'global.gossipEncryption.autoGenerate=false' \
      .
}

@test "gossipEncryptionAutogenerate/PodSecurityPolicy: disabled with global.gossipEncryption.autoGenerate=true and global.enablePodSecurityPolicies=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogenerate-podsecuritypolicy.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      .
}

@test "gossipEncryptionAutogenerate/PodSecurityPolicy: disabled with global.gossipEncryption.autoGenerate=false and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogenerate-podsecuritypolicy.yaml  \
      --set 'global.gossipEncryption.autoGenerate=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "gossipEncryptionAutogenerate/PodSecurityPolicy: enabled with global.gossipEncryption.autoGenerate=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-podsecuritypolicy.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
