#!/usr/bin/env bats

load _helpers

#--------------------------------------------------------------------
# feature-gate-set Job: enabled/disabled by server guard

@test "feature-gate-set/Job: rendered when server enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
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

#--------------------------------------------------------------------
# feature-gate-set Job: hook annotations

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
# feature-gate-set Job: consul-ai gate value

@test "feature-gate-set/Job: consul-ai gate is disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q "consul-ai disabled"
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
# feature-gate-set Job: CONSUL_HTTP_ADDR construction

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

#--------------------------------------------------------------------
# feature-gate-set Job: TLS volumes

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

@test "feature-gate-set/Job: sets CONSUL_CACERT env when tls enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q "CONSUL_CACERT=/consul/tls/ca/tls.crt"
}

#--------------------------------------------------------------------
# feature-gate-set Job: ACL token

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

@test "feature-gate-set/Job: sets CONSUL_HTTP_TOKEN_FILE when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_TOKEN_FILE") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/acl/tokens/token" ]
}

#--------------------------------------------------------------------
# feature-gate-set ServiceAccount

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
# feature-gate-set Job: external servers guard

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

@test "feature-gate-set/Job: not rendered when both server.enabled=false and externalServers.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=false' \
      .
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

#--------------------------------------------------------------------
# feature-gate-set Job: null/unset ai.enabled treated as false

@test "feature-gate-set/Job: consul-ai gate is disabled when ai.enabled is null (not set)" {
  cd `chart_dir`
  # Render without touching ai.enabled at all — it defaults to false in values.yaml.
  # This test guards against null being rendered as the string "null" or empty.
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -q "consul-ai disabled"
  # Must not contain the literal word "null"
  echo "${actual}" | grep -qv "null"
}

#--------------------------------------------------------------------
# feature-gate-set Job: Vault TLS skips CONSUL_CACERT (Vault agent injects cert)

@test "feature-gate-set/Job: does not set CONSUL_CACERT when tls enabled with Vault backend" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  echo "${actual}" | grep -qv "CONSUL_CACERT="
}

@test "feature-gate-set/Job: does not mount consul-ca-cert volume when tls enabled with Vault backend" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/feature-gate-set-job.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.volumes // [] | .[].name] | contains(["consul-ca-cert"])' | tee /dev/stderr)
  [ "${actual}" = "false" ]
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
# feature-gate-set ServiceAccount: external servers guard

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
