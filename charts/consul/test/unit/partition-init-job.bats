#!/usr/bin/env bats

load _helpers

@test "partitionInit/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      .
}

@test "partitionInit/Job: enabled with global.adminPartitions.enabled=true and servers = false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "partitionInit/Job: disabled with global.adminPartitions.enabled=true and servers = true" {
  cd `chart_dir`
 assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'server.enabled=true' \
      .
}

@test "partitionInit/Job: disabled with global.adminPartitions.enabled=true and global.enabled = true" {
  cd `chart_dir`
 assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enabled=true' \
      .
}

@test "partitionInit/Job: disabled with global.adminPartitions.enabled=false" {
  cd `chart_dir`
 assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'server.enabled=true' \
      .
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "partitionInit/Job: sets TLS flags when global.tls.enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | any(contains("-use-https"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | any(contains("-consul-ca-cert=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | any(contains("-server-port=8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "partitionInit/Job: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo-ca-cert' \
      --set 'global.tls.caCert.secretKey=key' \
      --set 'global.tls.caKey.secretName=foo-ca-key' \
      --set 'global.tls.caKey.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name=="consul-ca-cert")' | tee /dev/stderr)

  # check that the provided ca cert secret is attached as a volume
  local actual
  actual=$(echo $ca_cert_volume | jq -r '.secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-cert" ]

  # check that the volume uses the provided secret key
  actual=$(echo $ca_cert_volume | jq -r '.secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]
}

#--------------------------------------------------------------------
# global.acls.bootstrapToken

@test "partitionInit/Job: -bootstrap-token-file is not set by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].command | any(contains("-bootstrap-token-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the volume doesn't exist
  local actual=$(echo "$object" |
  yq '.spec.template.spec.volumes | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount doesn't exist
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].volumeMounts | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "partitionInit/Job: -bootstrap-token-file is not set when acls.bootstrapToken.secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].command | any(contains("-bootstrap-token-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the volume doesn't exist
  local actual=$(echo "$object" |
  yq '.spec.template.spec.volumes | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount doesn't exist
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].volumeMounts | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "partitionInit/Job: -bootstrap-token-file is not set when acls.bootstrapToken.secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretKey=key' \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("-bootstrap-token-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the volume doesn't exist
  local actual=$(echo "$object" |
  yq '.spec.template.spec.volumes | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount doesn't exist
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].volumeMounts | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "partitionInit/Job: -bootstrap-token-file is set when acls.bootstrapToken.secretKey and secretName are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \
      --set 'global.acls.bootstrapToken.secretKey=key' \
      . | tee /dev/stderr)

  # Test the -bootstrap-token-file flag is set.
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].command | any(contains("-bootstrap-token-file=/consul/acl/tokens/bootstrap-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume exists
  local actual=$(echo "$object" |
  yq '.spec.template.spec.volumes | map(select(.name == "bootstrap-token")) | length == 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount exists
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].volumeMounts | map(select(.name == "bootstrap-token")) | length == 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}