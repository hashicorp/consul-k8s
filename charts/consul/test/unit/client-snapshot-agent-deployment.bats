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
      --set 'client.enabled=true' \
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

@test "client/SnapshotAgentDeployment: when client.snapshotAgent.configSecret.secretKey!=null and client.snapshotAgent.configSecret.secretName=null, fail" {
    cd `chart_dir`
    run helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=' \
      --set 'client.snapshotAgent.configSecret.secretKey=bar' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "client.snapshotAgent.configSecret.secretKey and client.snapshotAgent.configSecret.secretName must both be specified." ]]
}

@test "client/SnapshotAgentDeployment: when client.snapshotAgent.configSecret.secretName!=null and client.snapshotAgent.configSecret.secretKey=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/client-snapshot-agent-deployment.yaml  \
        --set 'client.enabled=true' \
        --set 'client.snapshotAgent.enabled=true' \
        --set 'client.snapshotAgent.configSecret.secretName=foo' \
        --set 'client.snapshotAgent.configSecret.secretKey=' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "client.snapshotAgent.configSecret.secretKey and client.snapshotAgent.configSecret.secretName must both be specified." ]]
}

@test "client/SnapshotAgentDeployment: adds volume for snapshot agent config secret when secret is configured" {
  cd `chart_dir`
  local vol=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'client.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "snapshot-config")' | tee /dev/stderr)
  local actual
  actual=$(echo $vol | jq -r '. .name' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-config' ]

  actual=$(echo $vol | jq -r '. .secret.secretName' | tee /dev/stderr)
  [ "${actual}" = 'a/b/c/d' ]

  actual=$(echo $vol | jq -r '. .secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-agent-config' ]

  actual=$(echo $vol | jq -r '. .secret.items[0].path' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-config.json' ]
}

@test "client/SnapshotAgentDeployment: adds volume mount to snapshot container for snapshot agent config secret when secret is configured" {
  cd `chart_dir`
  local vol=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'client.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "snapshot-config")' | tee /dev/stderr)
  local actual
  actual=$(echo $vol | jq -r '. .name' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-config' ]

  actual=$(echo $vol | jq -r '. .readOnly' | tee /dev/stderr)
  [ "${actual}" = 'true' ]

  actual=$(echo $vol | jq -r '. .mountPath' | tee /dev/stderr)
  [ "${actual}" = '/consul/config' ]
}

@test "client/SnapshotAgentDeployment: set config-dir argument on snapshot agent command to volume mount" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'client.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2] | contains("-config-dir=/consul/config")' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

#--------------------------------------------------------------------
# tolerations

@test "client/SnapshotAgentDeployment: no tolerations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates tolerations when client.tolerations is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates priorityClassName when client.priorityClassName is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.priorityClassName=allow' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName | contains("allow")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "clientSnapshotAgent/Deployment: consul-logout preStop hook is added when ACLs are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].lifecycle.preStop.exec.command[2]] | any(contains("/bin/consul logout"))' | tee /dev/stderr)
  [ "${object}" = "true" ]
}

@test "clientSnapshotAgent/Deployment: CONSUL_HTTP_TOKEN_FILE is not set when acls are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[1].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "clientSnapshotAgent/Deployment: CONSUL_HTTP_TOKEN_FILE is set when acls are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[2].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "clientSnapshotAgent/Deployment: init container is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "snapshot-agent-acl-init" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].value] | any(contains("http://$(HOST_IP):8500"))' | tee /dev/stderr)
      echo $actual
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "clientSnapshotAgent/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "snapshot-agent-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("CONSUL_CACERT"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].value] | any(contains("https://$(HOST_IP):8501"))' | tee /dev/stderr)
      echo $actual
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[1] | any(contains("consul-ca-cert"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "clientSnapshotAgent/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command with Partitions enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=default' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "snapshot-agent-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-acl-auth-method=release-name-consul-k8s-component-auth-method"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-partition=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("CONSUL_CACERT"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].value] | any(contains("https://$(HOST_IP):8501"))' | tee /dev/stderr)
      echo $actual
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[1] | any(contains("consul-ca-cert"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "clientSnapshotAgent/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled and autoencrypt enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "snapshot-agent-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("CONSUL_CACERT"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].value] | any(contains("https://$(HOST_IP):8501"))' | tee /dev/stderr)
      echo $actual
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[1] | any(contains("consul-auto-encrypt-ca-cert"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "clientSnapshotAgent/Deployment: auto-encrypt init container is created and is the first init-container when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled and autoencrypt enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "get-auto-encrypt-client-ca" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "client/SnapshotAgentDeployment: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: populates nodeSelector when client.nodeSelector is populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
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
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"50m","memory":"50Mi"},"requests":{"cpu":"50m","memory":"50Mi"}}' ]
}

@test "client/SnapshotAgentDeployment: can set resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
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

@test "client/SnapshotAgentDeployment: if caCert is set command is modified correctly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.caCert=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2] | contains("cat <<EOF > /extra-ssl-certs/custom-ca.pem")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: if caCert is set extra-ssl-certs volumeMount is added" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.caCert=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr | yq -r '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo $object | jq -r '.volumes[0].name' | tee /dev/stderr)
  [ "${actual}" = "extra-ssl-certs" ]

  local actual=$(echo $object | jq -r '.containers[0].volumeMounts[0].name' | tee /dev/stderr)
  [ "${actual}" = "extra-ssl-certs" ]
}

@test "client/SnapshotAgentDeployment: if caCert is set SSL_CERT_DIR env var is set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.caCert=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr | yq -r '.spec.template.spec.containers[0].env[0]' | tee /dev/stderr)

  local actual=$(echo $object | jq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "SSL_CERT_DIR" ]
  local actual=$(echo $object | jq -r '.value' | tee /dev/stderr)
  [ "${actual}" = "/etc/ssl/certs:/extra-ssl-certs" ]
}

#--------------------------------------------------------------------
# license-autoload

@test "client/SnapshotAgentDeployment: adds volume for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","secret":{"secretName":"foo"}}' ]
}

@test "client/SnapshotAgentDeployment: adds volume mount for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","mountPath":"/consul/license","readOnly":true}' ]
}

@test "client/SnapshotAgentDeployment: adds env var for license path when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"CONSUL_LICENSE_PATH","value":"/consul/license/bar"}' ]
}

@test "client/SnapshotAgentDeployment: does not add license secret volume if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/SnapshotAgentDeployment: does not add license secret volume mount if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/SnapshotAgentDeployment: does not add license env if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# get-auto-encrypt-client-ca

@test "client/SnapshotAgentDeployment: get-auto-encrypt-client-ca uses server's stateful set address by default and passes ca cert" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").command | join(" ")' | tee /dev/stderr)

  # check server address
  actual=$(echo $command | jq ' . | contains("-server-addr=release-name-consul-server")')
  [ "${actual}" = "true" ]

  # check server port
  actual=$(echo $command | jq ' . | contains("-server-port=8501")')
  [ "${actual}" = "true" ]

  # check server's CA cert
  actual=$(echo $command | jq ' . | contains("-ca-file=/consul/tls/ca/tls.crt")')
  [ "${actual}" = "true" ]

  # check consul-api-timeout
  actual=$(echo $command | jq ' . | contains("-consul-api-timeout=5s")')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault

@test "client/SnapshotAgentDeployment: configures server CA to come from vault when vault is enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check annotations
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-init-first"]' | tee /dev/stderr)
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
}

@test "client/SnapshotAgentDeployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-snapshot-agent-deployment.yaml  \
    --set 'client.enabled=true' \
    --set 'client.snapshotAgent.enabled=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-snapshot-agent-deployment.yaml  \
    --set 'client.enabled=true' \
    --set 'client.snapshotAgent.enabled=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-snapshot-agent-deployment.yaml  \
    --set 'client.enabled=true' \
    --set 'client.snapshotAgent.enabled=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "client/SnapshotAgentDeployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-snapshot-agent-deployment.yaml  \
    --set 'client.enabled=true' \
    --set 'client.snapshotAgent.enabled=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
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

@test "client/SnapshotAgentDeployment: vault enterprise license annotations are correct when enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-snapshot-agent-deployment.yaml  \
    --set 'client.enabled=true' \
    --set 'client.snapshotAgent.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.enterpriseLicense.secretName=path/to/secret' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-enterpriselicense.txt"]' | tee /dev/stderr)
  [ "${actual}" = "path/to/secret" ]
  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-enterpriselicense.txt"]' | tee /dev/stderr)
  local actual="$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-enterpriselicense.txt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"path/to/secret\" -}}\n{{- .Data.data.enterpriselicense -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "client/SnapshotAgentDeployment: vault CONSUL_LICENSE_PATH is set to /vault/secrets/enterpriselicense.txt" {
  cd `chart_dir`
  local env=$(helm template \
    -s templates/client-snapshot-agent-deployment.yaml  \
    --set 'client.enabled=true' \
    --set 'client.snapshotAgent.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.enterpriseLicense.secretName=a/b/c/d' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
    . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_LICENSE_PATH") | .value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/enterpriselicense.txt" ]
}

@test "client/SnapshotAgentDeployment: vault does not add volume for license secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.enterpriseLicense.secretName=a/b/c/d' \
      --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/SnapshotAgentDeployment: vault does not add volume mount for license secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.enterpriseLicense.secretName=a/b/c/d' \
      --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/SnapshotAgentDeployment: vault snapshot agent config annotations are correct when enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-snapshot-agent-deployment.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulSnapshotAgentRole=bar' \
    --set 'client.snapshotAgent.enabled=true' \
    --set 'client.snapshotAgent.configSecret.secretName=path/to/secret' \
    --set 'client.snapshotAgent.configSecret.secretKey=config' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-snapshot-agent-config.json"]' | tee /dev/stderr)
  [ "${actual}" = "path/to/secret" ]

  actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-snapshot-agent-config.json"]' | tee /dev/stderr)
  local expected=$'{{- with secret \"path/to/secret\" -}}\n{{- .Data.data.config -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  actual=$(echo $object | jq -r '.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "client/SnapshotAgentDeployment: vault does not add volume for snapshot agent config secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'client.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "snapshot-config")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/SnapshotAgentDeployment: vault does not add volume mount for snapshot agent config secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'client.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "snapshot-config")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/SnapshotAgentDeployment: vault sets config-file argument on snapshot agent command to config downloaded by vault agent injector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'client.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2] | contains("-config-file=/vault/secrets/snapshot-agent-config.json")' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

#--------------------------------------------------------------------
# Vault agent annotations

@test "client/SnapshotAgentDeployment: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations | del(."consul.hashicorp.com/connect-inject") | del(."vault.hashicorp.com/agent-inject") | del(."vault.hashicorp.com/role")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "client/SnapshotAgentDeployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}


@test "client/SnapshotAgentDeployment: vault properly sets vault role when global.secretsBackend.vault.consulCARole is set but global.secretsBackend.vault.consulSnapshotAgentRole is not set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "ca-role" ]
}

@test "client/SnapshotAgentDeployment: vault properly sets vault role when global.secretsBackend.vault.consulSnapshotAgentRole is set but global.secretsBackend.vault.consulCARole is not set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulSnapshotAgentRole=sa-role' \
      --set 'client.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'client.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "sa-role" ]
}

@test "client/SnapshotAgentDeployment: vault properly sets vault role to global.secretsBackend.vault.consulSnapshotAgentRole value when both global.secretsBackend.vault.consulSnapshotAgentRole and global.secretsBackend.vault.consulCARole are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulSnapshotAgentRole=sa-role' \
      --set 'client.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'client.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      --set 'global.secretsBackend.vault.consulCARole=ca-role' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "sa-role" ]
}

@test "client/SnapshotAgentDeployment: interval defaults to 1h" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2] | contains("-interval=1h")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SnapshotAgentDeployment: interval can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-snapshot-agent-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'client.snapshotAgent.enabled=true' \
      --set 'client.snapshotAgent.interval=10h34m5s' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2] | contains("-interval=10h34m5s")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
