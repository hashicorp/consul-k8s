#!/usr/bin/env bats

load _helpers

@test "client/SnapshotAgentDeployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      .
}

@test "client/SnapshotAgentDeployment: enabled with client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: enabled with client.enabled=true and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: disabled with client=false and client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.enabled=false' \
      .
}

#--------------------------------------------------------------------
# tolerations

@test "client/SnapshotAgentDeployment: no tolerations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates tolerations when client.tolerations is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.tolerations=allow' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations | contains("allow")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "client/SnapshotAgentDeployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates priorityClassName when client.priorityClassName is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.priorityClassName=allow' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName | contains("allow")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs and snapshotAgent.configSecret

@test "client/SnapshotAgentDeployment: no initContainer by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/SnapshotAgentDeployment: populates initContainer when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: no volumes by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/SnapshotAgentDeployment: populates volumes when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: populates volumes when client.snapshotAgent.configSecret.secretName and client.snapshotAgent.configSecret secretKey are defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=secret' \
      --set 'client.snapshotAgent.configSecret.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: no container volumeMounts by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/SnapshotAgentDeployment: populates container volumeMounts when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: populates container volumeMounts when client.snapshotAgent.configSecret.secretName and client.snapshotAgent.configSecret secretKey are defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=secret' \
      --set 'client.snapshotAgent.configSecret.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "client/SnapshotAgentDeployment: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates nodeSelector when client.nodeSelector is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.nodeSelector=allow' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector | contains("allow")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "client/SnapshotAgentDeployment: sets TLS env vars when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'https://$(HOST_IP):8501' ]

  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "client/SnapshotAgentDeployment: populates volumes when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: populates container volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: can overwrite CA with the provided secret" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
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

  # check that it uses the provided secret key
  actual=$(echo $ca_cert_volume | jq -r '.secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]
}

#--------------------------------------------------------------------
# global.tls.enableAutoEncrypt

@test "client/SnapshotAgentDeployment: consul-auto-encrypt-ca-cert volume is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: consul-auto-encrypt-ca-cert volumeMount is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: get-auto-encrypt-client-ca init container is created when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: adds both init containers when TLS with auto-encrypt and ACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers | length == 2' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# resources

@test "client/SnapshotAgentDeployment: default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"50m","memory":"50Mi"},"requests":{"cpu":"50m","memory":"50Mi"}}' ]
}

@test "client/SnapshotAgentDeployment: can set resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.resources.requests.memory=100Mi' \
      --set 'client.snapshotAgent.resources.requests.cpu=100m' \
      --set 'client.snapshotAgent.resources.limits.memory=200Mi' \
      --set 'client.snapshotAgent.resources.limits.cpu=200m' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"200m","memory":"200Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}' ]
}

#--------------------------------------------------------------------
# client.snapshotAgent.caCert

@test "client/SnapshotAgentDeployment: if caCert is set it is used in command" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.caCert=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)

  exp='cat <<EOF > /etc/ssl/certs/custom-ca.pem
-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL
EOF
exec /bin/consul snapshot agent \'

  [ "${actual}" = "${exp}" ]
}

#--------------------------------------------------------------------
# license-autoload

@test "client/SnapshotAgentDeployment: adds volume for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","secret":{"secretName":"foo"}}' ]
}

@test "client/SnapshotAgentDeployment: adds volume mount for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","mountPath":"/consul/license","readOnly":true}' ]
}

@test "client/SnapshotAgentDeployment: adds env var for license path when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"CONSUL_LICENSE_PATH","value":"/consul/license/bar"}' ]
}

@test "client/SnapshotAgentDeployment: does not add license secret volume if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/SnapshotAgentDeployment: does not add license secret volume mount if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/SnapshotAgentDeployment: does not add license env if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}
