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
      --set 'global.adminPartitions.name=bar' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
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

@test "partitionInit/Job: disabled with global.adminPartitions.enabled=true and adminPartition.name = default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'server.enabled=false' \
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

@test "partitionInit/Job: fails if externalServers.enabled = false with non-default adminPartition" {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=false' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.enabled needs to be true and configured to create a non-default partition." ]]
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
      --set 'global.adminPartitions.name=bar' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
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

@test "partitionInit/Job: does not set consul ca cert or server-port when .externalServers.useSystemRoots is true" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | any(contains("-consul-ca-cert=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "partitionInit/Job: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
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

@test "partitionInit/Job: HTTP_TOKEN is set when global.acls.bootstrapToken is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.acls.bootstrapToken.secretName=partition-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# partition reserved name

@test "partitionInit/Job: fails when adminPartitions.name=system" {
  reservedNameTest "system"
}

@test "partitionInit/Job: fails when adminPartitions.name=universal" {
  reservedNameTest "universal"
}

@test "partitionInit/Job: fails when adminPartitions.name=consul" {
  reservedNameTest "consul"
}

@test "partitionInit/Job: fails when adminPartitions.name=operator" {
  reservedNameTest "operator"
}

@test "partitionInit/Job: fails when adminPartitions.name=root" {
  reservedNameTest "root"
}

# reservedNameTest is a helper function that tests if certain partition names
# fail because the name is reserved.
reservedNameTest() {
  cd `chart_dir`
  local -r name="$1"
		run helm template \
				-s templates/partition-init-job.yaml  \
				--set 'global.enabled=false' \
				--set 'externalServers.enabled=true' \
                --set 'externalServers.hosts[0]=foo' \
				--set 'global.adminPartitions.enabled=true' \
				--set "global.adminPartitions.name=$name" .

		[ "$status" -eq 1 ]
		[[ "$output" =~ "The name $name set for key global.adminPartitions.name is reserved by Consul for future use" ]]
}
