#!/usr/bin/env bats

load _helpers

#--------------------------------------------------------------------
# Rendering guard

@test "feature-gate-set/Job: rendered when server enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "feature-gate-set/Job: rendered when externalServers.enabled=true and server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.example.com' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "feature-gate-set/Job: not rendered when server.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      .
}

@test "feature-gate-set/Job: not rendered when global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.enabled=false' \
      .
}

@test "feature-gate-set/Job: not rendered when both server.enabled=false and externalServers.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=false' \
      .
}

@test "feature-gate-set/Job: not rendered when server.updatePartition > 0" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.updatePartition=1' \
      .
}

@test "feature-gate-set/Job: rendered when server.updatePartition=0" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.updatePartition=0' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "feature-gate-set/Job: rendered when externalServers.enabled=true and server.updatePartition > 0" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.example.com' \
      --set 'server.updatePartition=1' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Hook annotations

@test "feature-gate-set/Job: has post-install,post-upgrade hook" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations["helm.sh/hook"]' | tee /dev/stderr)
  [ "${actual}" = "post-install,post-upgrade" ]
}

@test "feature-gate-set/Job: hook-weight is 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations["helm.sh/hook-weight"]' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# consul-ai gate value

@test "feature-gate-set/Job: consul-ai gate is disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q "consul-ai disabled"
}

@test "feature-gate-set/Job: consul-ai gate is disabled when ai.enabled is null (not set)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q "consul-ai disabled"
  echo "${actual}" | grep -qv "null"
}

@test "feature-gate-set/Job: consul-ai gate is disabled when ai.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'ai.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q "consul-ai disabled"
}

@test "feature-gate-set/Job: consul-ai gate is enabled when ai.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'ai.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q "consul-ai enabled"
}

#--------------------------------------------------------------------
# CONSUL_HTTP_ADDR construction

@test "feature-gate-set/Job: uses http scheme when tls disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q 'CONSUL_HTTP_ADDR="http://'
}

@test "feature-gate-set/Job: uses https scheme when tls enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q 'CONSUL_HTTP_ADDR="https://'
}

@test "feature-gate-set/Job: uses externalServers address in CONSUL_ADDRESSES when externalServers.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.example.com' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_ADDRESSES") | .value' | tee /dev/stderr)
  [ "${actual}" = "consul.example.com" ]
}

@test "feature-gate-set/Job: rejects missing external server hosts" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"externalServers.hosts must be set"* ]]
}

@test "feature-gate-set/Job: rejects exec-based external server hosts" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set-string 'externalServers.hosts[0]=exec=echo 127.0.0.1' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"DNS name or IP address; exec= addresses are not supported"* ]]
}

@test "feature-gate-set/Job: brackets IPv6 hosts in the HTTP URL" {
  cd `chart_dir`
  local script=$(helm template -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set-string 'externalServers.hosts[0]=2001:db8::1' \
      --set 'global.tls.enabled=true' \
      . | yq -r '.spec.template.spec.containers[0].command[2]')
  local actual=$(CONSUL_ADDRESSES=2001:db8::1 CONSUL_HTTP_PORT=8501 \
      /bin/sh -ec 'consul() { printf "%s\n" "$CONSUL_HTTP_ADDR"; }; '"$script")
  [ "$actual" = "Setting feature gate: consul-ai=disabled
https://[2001:db8::1]:8501" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "feature-gate-set/Job: no TLS volumes by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "feature-gate-set/Job: mounts consul-ca-cert volume when tls enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '[.spec.template.spec.volumes[].name] | contains(["consul-ca-cert"])' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "feature-gate-set/Job: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo-ca-cert' \
      --set 'global.tls.caCert.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name=="consul-ca-cert")' | tee /dev/stderr)

  local actual
  actual=$(echo "$ca_cert_volume" | yq -r '.secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-cert" ]

  actual=$(echo "$ca_cert_volume" | yq -r '.secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]
}

@test "feature-gate-set/Job: sets CONSUL_CACERT env when tls enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q "CONSUL_CACERT=/consul/tls/ca/tls.crt"
}

@test "feature-gate-set/Job: does not mount consul-ca-cert when externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.example.com' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.volumes // [] | .[].name] | contains(["consul-ca-cert"])' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "feature-gate-set/Job: does not set CONSUL_CACERT when externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.example.com' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -qv "CONSUL_CACERT="
}

#--------------------------------------------------------------------
# global.acls.bootstrapToken (externally-managed ACLs)

@test "feature-gate-set/Job: CONSUL_HTTP_TOKEN is set via secretKeyRef when bootstrapToken is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=my-token-secret' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN") | .name' | tee /dev/stderr)
  [ "${actual}" = "CONSUL_HTTP_TOKEN" ]
}

@test "feature-gate-set/Job: CONSUL_HTTP_TOKEN secretKeyRef name and key match bootstrapToken" {
  cd `chart_dir`
  local actual
  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=my-token-secret' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN") | .valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "my-token-secret" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=my-token-secret' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN") | .valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "token" ]
}

@test "feature-gate-set/Job: no CONSUL_HTTP_TOKEN env when bootstrapToken not set and no manageSystemACLs" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN" or .name == "CONSUL_HTTP_TOKEN_FILE")] | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "feature-gate-set/Job: ignores replication token when ACL management is disabled" {
  cd `chart_dir`
  local job=$(helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.replicationToken.secretName=replication-token' \
      --set 'global.acls.replicationToken.secretKey=token' \
      . | yq '.')
  [ "$(echo "$job" | yq '[.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN" or .name == "CONSUL_HTTP_TOKEN_FILE")] | length')" = "0" ]
  [ "$(echo "$job" | yq '[.spec.template.spec.volumes // [] | .[].name] | contains(["bootstrap-acl-token"])')" = "false" ]
}

@test "feature-gate-set/Job: rejects a bootstrap token without a secret key" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=bootstrap-token' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"both global.acls.bootstrapToken.secretKey and global.acls.bootstrapToken.secretName must be set"* ]]
}

@test "feature-gate-set/Job: rejects a bootstrap token key without a secret name" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretKey=token' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"both global.acls.bootstrapToken.secretKey and global.acls.bootstrapToken.secretName must be set"* ]]
}

@test "feature-gate-set/Job: does not mount bootstrap-acl-token volume when bootstrapToken set inline (no Vault, no manageSystemACLs)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=my-token-secret' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.volumes // [] | .[].name] | contains(["bootstrap-acl-token"])' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs (chart-managed ACLs)

@test "feature-gate-set/Job: no bootstrap-acl-token volume when ACLs disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.volumes // [] | .[].name] | contains(["bootstrap-acl-token"])' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "feature-gate-set/Job: mounts bootstrap-acl-token when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '[.spec.template.spec.volumes[].name] | contains(["bootstrap-acl-token"])' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "feature-gate-set/Job: sets CONSUL_HTTP_TOKEN_FILE to /consul/acl/tokens/token when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN_FILE") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/acl/tokens/token" ]
}

@test "feature-gate-set/Job: uses the replication token without a bootstrap token in a secondary datacenter" {
  cd `chart_dir`
  local job=$(helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=consul-ca-cert' \
      --set 'meshGateway.enabled=true' \
      --set 'global.acls.replicationToken.secretName=replication-token' \
      --set 'global.acls.replicationToken.secretKey=token' \
      . | yq '.')
  [ "$(echo "$job" | yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN") | .valueFrom.secretKeyRef.name')" = "replication-token" ]
  [ "$(echo "$job" | yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN") | .valueFrom.secretKeyRef.key')" = "token" ]
  [ "$(echo "$job" | yq -r '[.spec.template.spec.volumes // [] | .[].name] | contains(["bootstrap-acl-token"])')" = "false" ]
}

@test "feature-gate-set/Job: explicit bootstrap token takes precedence over replication token" {
  cd `chart_dir`
  local job=$(helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      --set 'global.acls.replicationToken.secretName=replication-token' \
      --set 'global.acls.replicationToken.secretKey=token' \
      . | yq '.')
  [ "$(echo "$job" | yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN") | .valueFrom.secretKeyRef.name')" = "bootstrap-token" ]
  [ "$(echo "$job" | yq '[.spec.template.spec.volumes // [] | .[].name] | contains(["bootstrap-acl-token"])')" = "false" ]
}

@test "feature-gate-set/Job: rejects a replication token without a secret key" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretName=replication-token' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"both global.acls.replicationToken.secretKey and global.acls.replicationToken.secretName must be set"* ]]
}

@test "feature-gate-set/Job: rejects a replication token key without a secret name" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretKey=token' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"both global.acls.replicationToken.secretKey and global.acls.replicationToken.secretName must be set"* ]]
}

#--------------------------------------------------------------------
# Vault — TLS only (no ACLs)

@test "feature-gate-set/Job: does not set CONSUL_CACERT when tls enabled with Vault backend" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -qv "CONSUL_CACERT="
}

@test "feature-gate-set/Job: does not mount consul-ca-cert volume when tls enabled with Vault backend" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.volumes // [] | .[].name] | contains(["consul-ca-cert"])' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "feature-gate-set/Job: configures server CA to come from vault when vault and TLS are enabled" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local actual
  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/agent-pre-populate-only"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "ca-role" ]

  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = "pki/ca" ]

  # No volumes or volumeMounts needed — Vault agent injects the cert
  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

#--------------------------------------------------------------------
# Vault — ACLs only (no TLS)

@test "feature-gate-set/Job: configures vault annotations when ACLs are enabled but TLS disabled" {
  cd `chart_dir`
  local actual
  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-pre-populate-only"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "acl-role" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject-secret-bootstrap-token"]' | tee /dev/stderr)
  [ "${actual}" = "consul/bootstrap-token" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name=="CONSUL_HTTP_TOKEN_FILE") | .value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/bootstrap-token" ]

  # No secret volumes — Vault agent injects the token
  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "feature-gate-set/Job: does not mount bootstrap-acl-token volume when Vault backend and ACLs enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.volumes // [] | .[].name] | contains(["bootstrap-acl-token"])' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "feature-gate-set/Job: sets CONSUL_HTTP_TOKEN_FILE to vault path when Vault backend and ACLs enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN_FILE") | .value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/bootstrap-token" ]
}

#--------------------------------------------------------------------
# Vault — ACLs + TLS

@test "feature-gate-set/Job: configures vault annotations when both ACLs and TLS are enabled" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local actual
  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/agent-pre-populate-only"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # The feature-gate role takes precedence over the CA role when both are set
  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "acl-role" ]

  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = "pki/ca" ]

  actual=$(echo "$annotations" | yq -r '.["vault.hashicorp.com/agent-inject-secret-bootstrap-token"]' | tee /dev/stderr)
  [ "${actual}" = "consul/bootstrap-token" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name=="CONSUL_HTTP_TOKEN_FILE") | .value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/bootstrap-token" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]

  actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

#--------------------------------------------------------------------
# Vault — bootstrapToken explicit (externally-managed ACLs) + Vault

@test "feature-gate-set/Job: adds vault agent annotations when bootstrapToken set with Vault (no manageSystemACLs)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=vault/path/token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "feature-gate-set/Job: sets CONSUL_HTTP_TOKEN_FILE to vault path when bootstrapToken set with Vault (no manageSystemACLs)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=vault/path/token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.featureGateSetRole=acl-role' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN_FILE") | .value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/bootstrap-token" ]
}

@test "feature-gate-set/Job: vault agent uses featureGateSetRole" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=server-acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=my-acl-role' \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "my-acl-role" ]
}

@test "feature-gate-set/Job: requires its own Vault role for managed ACLs" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/data/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=server-role' \
      --set 'global.secretsBackend.vault.consulClientRole=client-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=server-acl-role' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"global.secretsBackend.vault.featureGateSetRole is required"* ]]
}

@test "feature-gate-set/Job: requires its own Vault role for externally managed ACLs" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=server-role' \
      --set 'global.secretsBackend.vault.consulClientRole=client-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=server-acl-role' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"global.secretsBackend.vault.featureGateSetRole is required"* ]]
}

@test "feature-gate-set/Job: uses its own Vault role for externally managed ACLs" {
  cd `chart_dir`
  local actual=$(helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.bootstrapToken.secretName=consul/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=server-role' \
      --set 'global.secretsBackend.vault.consulClientRole=client-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=server-acl-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=feature-gate-role' \
      . | yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/role"]')
  [ "$actual" = "feature-gate-role" ]
}

@test "feature-gate-set/Job: requires a token source for Vault-managed ACLs" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=server-role' \
      --set 'global.secretsBackend.vault.consulClientRole=client-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-init-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=feature-gate-role' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"global.acls.bootstrapToken or global.acls.replicationToken must be provided"* ]]
}

@test "feature-gate-set/Job: requires its own Vault role for replication-only ACLs" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretName=consul/data/replication-token' \
      --set 'global.acls.replicationToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=server-role' \
      --set 'global.secretsBackend.vault.consulClientRole=client-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-init-role' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"global.secretsBackend.vault.featureGateSetRole is required"* ]]
}

@test "feature-gate-set/Job: Vault bootstrap token takes precedence over replication token" {
  cd `chart_dir`
  local job=$(helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=consul/data/bootstrap-token' \
      --set 'global.acls.bootstrapToken.secretKey=token' \
      --set 'global.acls.replicationToken.secretName=consul/data/replication-token' \
      --set 'global.acls.replicationToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=server-role' \
      --set 'global.secretsBackend.vault.consulClientRole=client-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-init-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=feature-gate-role' \
      . | yq '.')
  [ "$(echo "$job" | yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject-secret-bootstrap-token"]')" = "consul/data/bootstrap-token" ]
  [ "$(echo "$job" | yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject-secret-replication-token"]')" = "null" ]
  [ "$(echo "$job" | yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN_FILE") | .value')" = "/vault/secrets/bootstrap-token" ]
}

@test "feature-gate-set/Job: injects the replication token from Vault in a secondary datacenter" {
  cd `chart_dir`
  local job=$(helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=consul-ca-cert' \
      --set 'meshGateway.enabled=true' \
      --set 'global.acls.replicationToken.secretName=consul/data/replication-token' \
      --set 'global.acls.replicationToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=server-role' \
      --set 'global.secretsBackend.vault.consulClientRole=client-role' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-init-role' \
      --set 'global.secretsBackend.vault.featureGateSetRole=feature-gate-role' \
      . | yq '.')
  [ "$(echo "$job" | yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject-secret-replication-token"]')" = "consul/data/replication-token" ]
  [ "$(echo "$job" | yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject-secret-bootstrap-token"]')" = "null" ]
  [ "$(echo "$job" | yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject-template-replication-token"]' | grep -c 'Data.data.token')" -gt 0 ]
  [ "$(echo "$job" | yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN_FILE") | .value')" = "/vault/secrets/replication-token" ]
  [ "$(echo "$job" | yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/role"]')" = "feature-gate-role" ]
}

@test "feature-gate-set/Job: vault agent uses consulCARole when only TLS enabled (no ACLs)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=my-ca-role' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "my-ca-role" ]
}

@test "feature-gate-set/Job: requires consulCARole for Vault TLS without ACL token" {
  cd `chart_dir`
  run helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' .
  [ "$status" -eq 1 ]
  [[ "$output" == *"global.secretsBackend.vault.consulCARole is required"* ]]
}

@test "feature-gate-set/Job: no vault agent annotations when ACLs disabled even with Vault enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "feature-gate-set/Job: ignores replication token in Vault when ACL management is disabled" {
  cd `chart_dir`
  local job=$(helm template -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.replicationToken.secretName=consul/data/replication-token' \
      --set 'global.acls.replicationToken.secretKey=token' \
      --set 'global.secretsBackend.vault.enabled=true' \
      . | yq '.')
  [ "$(echo "$job" | yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/agent-inject"]')" = "null" ]
  [ "$(echo "$job" | yq '[.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN" or .name == "CONSUL_HTTP_TOKEN_FILE")] | length')" = "0" ]
}

#--------------------------------------------------------------------
# Vault CA annotations

@test "feature-gate-set/Job: vault CA is not configured by default" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo "$annotations" | yq -r 'has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo "$annotations" | yq -r 'has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "feature-gate-set/Job: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.ca.secretName=ca' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo "$annotations" | yq -r 'has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo "$annotations" | yq -r 'has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "feature-gate-set/Job: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.ca.secretName=ca' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo "$annotations" | yq -r '."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo "$annotations" | yq -r '."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

#--------------------------------------------------------------------
# Vault namespace annotation

@test "feature-gate-set/Job: vault namespace annotation is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)
  [ "${actual}" = "vns" ]
}

@test "feature-gate-set/Job: vault namespace annotation not set when agentAnnotations already contains it" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/namespace: override' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)
  [ "${actual}" = "override" ]
}

#--------------------------------------------------------------------
# Vault agent annotations passthrough

@test "feature-gate-set/Job: no extra vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
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

@test "feature-gate-set/Job: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=pki/ca' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# ServiceAccount

@test "feature-gate-set/ServiceAccount: rendered when server enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-serviceaccount.yaml \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "feature-gate-set/ServiceAccount: not rendered when server.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/feature-gate-set-serviceaccount.yaml \
      --set 'server.enabled=false' \
      .
}

@test "feature-gate-set/ServiceAccount: rendered when externalServers.enabled=true and server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-serviceaccount.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.example.com' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "feature-gate-set/ServiceAccount: not rendered when both server.enabled=false and externalServers.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/feature-gate-set-serviceaccount.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=false' \
      .
}

@test "feature-gate-set/ServiceAccount: name matches Job serviceAccountName" {
  cd `chart_dir`
  local sa=$(helm template \
      -s templates/feature-gate-set-serviceaccount.yaml \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  local job_sa=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.serviceAccountName' | tee /dev/stderr)
  [ "${sa}" = "${job_sa}" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "feature-gate-set/Job: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "feature-gate-set/Job: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

#--------------------------------------------------------------------
# global.acls.nodeSelector

@test "feature-gate-set/Job: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "feature-gate-set/Job: nodeSelector is set when global.acls.nodeSelector is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.nodeSelector=kubernetes.io/arch: amd64' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector."kubernetes.io/arch"' | tee /dev/stderr)
  [ "${actual}" = "amd64" ]
}

#--------------------------------------------------------------------
# global.acls.resources

@test "feature-gate-set/Job: uses default resources when global.acls.resources not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.requests.memory' | tee /dev/stderr)
  [ "${actual}" = "50Mi" ]
}

@test "feature-gate-set/Job: overrides resources when global.acls.resources is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.resources.requests.memory=100Mi' \
      --set 'global.acls.resources.limits.memory=100Mi' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.requests.memory' | tee /dev/stderr)
  [ "${actual}" = "100Mi" ]
}
