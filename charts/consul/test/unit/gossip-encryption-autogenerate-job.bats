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
# global.gossipEncryption.tolerations and global.gossipEncryption.nodeSelector

@test "gossipEncryptionAutogenerate/Job: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "gossipEncryptionAutogenerate/Job: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.gossipEncryption.tolerations=- key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "gossipEncryptionAutogenerate/Job: nodeSelector not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "gossipEncryptionAutogenerate/Job: nodeSelector can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.gossipEncryption.nodeSelector=- key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "gossipEncryptionAutogenerate/Job: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "gossipEncryptionAutogenerate/Job: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "gossipEncryptionAutogenerate/Job: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.extraLabels.foo=bar' \
      --set 'global.extraLabels.baz=qux' \
      . | tee /dev/stderr)
  local actualFoo=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  local actualBaz=$(echo "${actual}" | yq -r '.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualFoo}" = "bar" ]
  [ "${actualBaz}" = "qux" ]
  local actualTemplateFoo=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  local actualTemplateBaz=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualTemplateFoo}" = "bar" ]
  [ "${actualTemplateBaz}" = "qux" ]
}

#--------------------------------------------------------------------
# logLevel

@test "gossipEncryptionAutogenerate/Job: uses the global.logLevel flag by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "gossipEncryptionAutogenerate/Job: overrides the global.logLevel flag when global.gossipEncryption.logLevel is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/gossip-encryption-autogenerate-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.gossipEncryption.logLevel=debug' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
