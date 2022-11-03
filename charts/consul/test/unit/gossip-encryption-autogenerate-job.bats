#!/usr/bin/env bats

load _helpers

@test "gossipEncryptionAutogenerate/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      .
}

@test "gossipEncryptionAutogenerate/Job: enabled with global.gossipEncryption.autoGenerate=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "gossipEncryptionAutogenerate/Job: disabled when global.gossipEncryption.autoGenerate=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=false' \
      .
}

@test "gossipEncryptionAutogenerate/Job: fails if global.gossipEncryption.autoGenerate=true and global.gossipEncryption.secretName and global.gossipEncryption.secretKey are set" {
  cd `chart_dir`
  run helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.gossipEncryption.secretName=name' \
      --set 'global.gossipEncryption.secretKey=key' \
      . 
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.gossipEncryption.autoGenerate is true, global.gossipEncryption.secretName and global.gossipEncryption.secretKey must not be set." ]]
}

@test "gossipEncryptionAutogenerate/Job: fails if global.gossipEncryption.autoGenerate=true and global.gossipEncryption.secretName+key are set" {
  cd `chart_dir`
  run helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.gossipEncryption.secretName=name' \
      --set 'global.gossipEncryption.secretKey=name' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.gossipEncryption.autoGenerate is true, global.gossipEncryption.secretName and global.gossipEncryption.secretKey must not be set." ]]
}

@test "gossipEncryptionAutogenerate/Job: securityContext is not set when global.openshift.enabled=true" {
  cd `chart_dir`
  local has_security_context=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec | has("securityContext")' | tee /dev/stderr)
  [ "${has_security_context}" = "false" ]
}

#--------------------------------------------------------------------
# podSecurityStandards

@test "gossipEncryptionAutogenerate/Job: podSecurityStandards default off" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "gossipEncryptionAutogenerate/Job: global.podSecurityStandards are not set when global.openshift.enabled=true" {
  cd `chart_dir`
  local manifest=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr)

  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.containers | map(select(.name == "apply-gossip-encryption-autogenerate")) | .[0].securityContext')
  [ "${actual}" = "null" ]
}

@test "gossipEncryptionAutogenerate/Job: global.podSecurityStandards can be set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.podSecurityStandards.securityContext.bob=false' \
      --set 'global.podSecurityStandards.securityContext.alice=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.containers | map(select(.name=="gossip-encryption-autogen")) | .[0].securityContext' | jq -r .bob)
  [ "${actual}" = "false" ]
  local actual=$(echo $object |
      yq -r '.containers | map(select(.name=="gossip-encryption-autogen")) | .[0].securityContext' | jq -r .alice)
  [ "${actual}" = "true" ]
}
