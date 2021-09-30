#!/usr/bin/env bats

load _helpers

@test "gossipEncryptionAutogeneration/ServiceAccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogeneration-serviceaccount.yaml  \
      .
}

@test "gossipEncryptionAutogeneration/ServiceAccount: disabled with global.gossipEncryption.autoGenerate=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogeneration-serviceaccount.yaml  \
      --set 'global.gossipEncryption.autoGenerate=false' \
      .
}

@test "gossipEncryptionAutogeneration/ServiceAccount: enabled with global.gossipEncryption.autoGenerate=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogeneration-serviceaccount.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "gossipEncryptionAutogeneration/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/gossip-encryption-autogeneration-serviceaccount.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
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

