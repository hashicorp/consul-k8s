
#!/usr/bin/env bats

load _helpers

@test "gossipEncryptionAutogeneration/RoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogeneration-rolebinding.yaml  \
      .
}

@test "gossipEncryptionAutogeneration/RoleBinding: disabled with global.gossipEncryption.autoGenerate=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogeneration-rolebinding.yaml  \
      --set 'global.gossipEncryption.autoGenerate=false' \
      .
}

@test "gossipEncryptionAutogeneration/RoleBinding: enabled with global.gossipEncryption.autoGenerate=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogeneration-rolebinding.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}