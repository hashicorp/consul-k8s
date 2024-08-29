#!/usr/bin/env bats

load _helpers

@test "partitionInit/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      . 
}

@test "partitionInit/Job: enabled with global.adminPartitions.enabled=true and server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
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
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=true' \
      .
}

@test "partitionInit/Job: disabled with global.adminPartitions.enabled=true and adminPartition.name = default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=false' \
      .
}

@test "partitionInit/Job: disabled with global.adminPartitions.enabled=true and global.enabled = true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.enabled=true' \
      .
}

@test "partitionInit/Job: disabled with global.adminPartitions.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=false' \
      .
}

@test "partitionInit/Job: fails if externalServers.enabled = false with non-default adminPartition" {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=false' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.enabled needs to be true and configured to create a non-default partition." ]]
}

@test "partitionInit/Job: consul env defaults" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/partition-init-job.yaml \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_ADDRESSES").value' | tee /dev/stderr)
  [ "${actual}" = "foo" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_GRPC_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8502" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_HTTP_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8501" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_DATACENTER").value' | tee /dev/stderr)
  [ "${actual}" = "dc1" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_API_TIMEOUT").value' | tee /dev/stderr)
  [ "${actual}" = "5s" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "partitionInit/Job: sets TLS env vars when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_HTTP_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8501" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_USE_TLS").value' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_CACERT_FILE").value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "partitionInit/Job: does not set consul ca cert when .externalServers.useSystemRoots is true" {
  cd `chart_dir`
  local spec=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '.containers[0].env[] | select( .name == "CONSUL_CACERT_FILE").value' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo "$env" |
    jq -r '.volumes[] | select( .name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo "$env" |
    jq -r '.spec.volumeMounts[] | select( .name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "partitionInit/Job: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'global.enableConsulNamespaces=true' \
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

@test "partitionInit/Job: CONSUL_ACL_TOKEN is set when global.acls.bootstrapToken is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.acls.bootstrapToken.secretName=partition-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
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

#--------------------------------------------------------------------
# Vault

@test "partitionInit/Job: fails when vault and ACLs are enabled but adminPartitionsRole is not provided" {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=boot' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=test' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.secretsBackend.vault.adminPartitionsRole is required when global.secretsBackend.vault.enabled and global.acls.manageSystemACLs are true." ]]
}

@test "partitionInit/Job: configures vault annotations when ACLs are enabled but TLS disabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'global.enableConsulNamespaces=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.adminPartitionsRole=aprole' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=foo' \
      --set 'global.acls.bootstrapToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check annotations
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-pre-populate-only"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "aprole" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-secret-bootstrap-token"')
  [ "${actual}" = "foo" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-template-bootstrap-token"')
  local expected=$'{{- with secret \"foo\" -}}\n{{- .Data.data.bar -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  # Check that the bootstrap token flag is set to the path of the Vault secret.
  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name=="partition-init-job").env[] | select(.name=="CONSUL_ACL_TOKEN_FILE").value')
  [ "${actual}" = "/vault/secrets/bootstrap-token" ]

  # Check that no (secret) volumes are not attached
  local actual=$(echo $object | jq -r '.spec.volumes')
  [ "${actual}" = "null" ]

  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name=="partition-init-job").volumeMounts')
  [ "${actual}" = "null" ]
}

@test "partitionInit/Job: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'global.enableConsulNamespaces=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "vns" ]
}

@test "partitionInit/Job: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set without vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/agent-extra-secret: bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "vns" ]
}

@test "partitionInit/Job: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/namespace: bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "bar" ]
}

@test "partitionInit/Job: configures server CA to come from vault when vault and TLS are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'global.enableConsulNamespaces=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check annotations
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-pre-populate-only"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "carole" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = $'{{- with secret \"foo\" -}}\n{{- .Data.certificate -}}\n{{- end -}}' ]

  # Check that the consul-ca-cert volume is not attached
  local actual=$(echo $object | jq -r '.spec.volumes')
  [ "${actual}" = "null" ]

  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name=="partition-init-job").volumeMounts')
  [ "${actual}" = "null" ]
}

@test "partitionInit/Job: configures vault annotations when both ACLs and TLS are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'global.enableConsulNamespaces=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
      --set 'global.secretsBackend.vault.adminPartitionsRole=aprole' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=foo' \
      --set 'global.acls.bootstrapToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check annotations
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-pre-populate-only"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "aprole" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = $'{{- with secret \"foo\" -}}\n{{- .Data.certificate -}}\n{{- end -}}' ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-secret-bootstrap-token"')
  [ "${actual}" = "foo" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-template-bootstrap-token"')
  local expected=$'{{- with secret \"foo\" -}}\n{{- .Data.data.bar -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  # Check that the bootstrap token flag is set to the path of the Vault secret.
  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name=="partition-init-job").env[] | select(.name=="CONSUL_ACL_TOKEN_FILE").value')
  [ "${actual}" = "/vault/secrets/bootstrap-token" ]

  # Check that the consul-ca-cert volume is not attached
  local actual=$(echo $object | jq -r '.spec.volumes')
  [ "${actual}" = "null" ]

  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name=="partition-init-job").volumeMounts')
  [ "${actual}" = "null" ]
}

@test "partitionInit/Job: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/partition-init-job.yaml  \
    --set 'global.enabled=false' \
    --set 'global.adminPartitions.enabled=true' \
    --set "global.adminPartitions.name=bar" \
    --set 'global.enableConsulNamespaces=true' \
    --set 'externalServers.enabled=true' \
    --set 'externalServers.hosts[0]=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "partitionInit/Job: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/partition-init-job.yaml  \
    --set 'global.enabled=false' \
    --set 'global.adminPartitions.enabled=true' \
    --set "global.adminPartitions.name=bar" \
    --set 'global.enableConsulNamespaces=true' \
    --set 'externalServers.enabled=true' \
    --set 'externalServers.hosts[0]=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "partitionInit/Job: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/partition-init-job.yaml  \
    --set 'global.enabled=false' \
    --set 'global.adminPartitions.enabled=true' \
    --set "global.adminPartitions.name=bar" \
    --set 'global.enableConsulNamespaces=true' \
    --set 'externalServers.enabled=true' \
    --set 'externalServers.hosts[0]=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "partitionInit/Job: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/partition-init-job.yaml  \
    --set 'global.enabled=false' \
    --set 'global.adminPartitions.enabled=true' \
    --set "global.adminPartitions.name=bar" \
    --set 'global.enableConsulNamespaces=true' \
    --set 'externalServers.enabled=true' \
    --set 'externalServers.hosts[0]=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

#--------------------------------------------------------------------
# Vault agent annotations

@test "partitionInit/Job: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'global.enableConsulNamespaces=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."vault.hashicorp.com/agent-inject") |
      del(."vault.hashicorp.com/agent-pre-populate-only") |
      del(."vault.hashicorp.com/role") |
      del(."vault.hashicorp.com/agent-inject-secret-serverca.crt") |
      del(."vault.hashicorp.com/agent-inject-template-serverca.crt")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "partitionInit/Job: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'global.enableConsulNamespaces=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# global.cloud

@test "partitionInit/Job: fails when global.cloud.enabled is true and global.cloud.clientId.secretName is not set but global.cloud.clientSecret.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientSecret.secretName=client-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=client-resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=client-resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When either global.cloud.resourceId.secretName or global.cloud.resourceId.secretKey is defined, both must be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.authUrl.secretName=auth-url-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.authUrl.secretName or global.cloud.authUrl.secretKey is defined, both must be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.authUrl.secretKey=auth-url-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.authUrl.secretName or global.cloud.authUrl.secretKey is defined, both must be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.apiHost.secretName=auth-url-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.apiHost.secretName or global.cloud.apiHost.secretKey is defined, both must be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.apiHost.secretKey=auth-url-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.apiHost.secretName or global.cloud.apiHost.secretKey is defined, both must be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.scadaAddress.secretName=scada-address-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.scadaAddress.secretName or global.cloud.scadaAddress.secretKey is defined, both must be set." ]]
}

@test "partitionInit/Job: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.scadaAddress.secretKey=scada-address-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.scadaAddress.secretName or global.cloud.scadaAddress.secretKey is defined, both must be set." ]]
}

@test "partitionInit/Job: sets TLS server name if global.cloud.enabled is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml  \
      --set 'global.enabled=false' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set "global.adminPartitions.name=bar" \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-server-name=server.dc1.consul"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "partitionInit/Job: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=false' \
      --set 'global.adminPartitions.name=bar' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "partitionInit/Job: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=false' \
      --set 'global.adminPartitions.name=bar' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "partitionInit/Job: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/partition-init-job.yaml \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'server.enabled=false' \
      --set 'global.adminPartitions.name=bar' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
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
