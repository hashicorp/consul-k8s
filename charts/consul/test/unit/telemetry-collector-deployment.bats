#!/usr/bin/env bats

load _helpers

@test "telemetryCollector/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      .
}

@test "telemetryCollector/Deployment: fails if no image is set" {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=null' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "telemetryCollector.image must be set to enable consul-telemetry-collector" ]]
}

@test "telemetryCollector/Deployment: disable with telemetry-collector.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=false' \
      .
}

@test "telemetryCollector/Deployment: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "telemetryCollector/Deployment: container image overrides" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "\"bar\"" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "telemetryCollector/Deployment: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "telemetryCollector/Deployment: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# consul.name

@test "telemetryCollector/Deployment: name is constant regardless of consul name" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'consul.name=foobar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].name' | tee /dev/stderr)
  [ "${actual}" = "consul-telemetry-collector" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "telemetryCollector/Deployment: Adds tls-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "telemetryCollector/Deployment: Adds tls-ca-cert volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[1].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "telemetryCollector/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
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
# global.tls.enableAutoEncrypt

@test "telemetryCollector/Deployment: consul-ca-cert volumeMount is added when TLS with auto-encrypt is enabled without clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[1].volumeMounts[] | select(.name == "consul-ca-cert") | length > 0' | tee
      /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Deployment: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# resources

@test "telemetryCollector/Deployment: resources has default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "512Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "1000m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "512Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "1000m" ]
}

@test "telemetryCollector/Deployment: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'telemetryCollector.resources.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# init container resources

@test "telemetryCollector/Deployment: init container has default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "25Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "50m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "150Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "50m" ]
}

@test "telemetryCollector/Deployment: init container resources can be set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'telemetryCollector.initContainer.resources.requests.memory=memory' \
      --set 'telemetryCollector.initContainer.resources.requests.cpu=cpu' \
      --set 'telemetryCollector.initContainer.resources.limits.memory=memory2' \
      --set 'telemetryCollector.initContainer.resources.limits.cpu=cpu2' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].resources' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.requests.memory' | tee /dev/stderr)
  [ "${actual}" = "memory" ]

  local actual=$(echo $object | yq -r '.requests.cpu' | tee /dev/stderr)
  [ "${actual}" = "cpu" ]

  local actual=$(echo $object | yq -r '.limits.memory' | tee /dev/stderr)
  [ "${actual}" = "memory2" ]

  local actual=$(echo $object | yq -r '.limits.cpu' | tee /dev/stderr)
  [ "${actual}" = "cpu2" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "telemetryCollector/Deployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "telemetryCollector/Deployment: can set a priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'telemetryCollector.priorityClassName=name' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "name" ]
}

#--------------------------------------------------------------------
# replicas

@test "telemetryCollector/Deployment: replicas defaults to 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "1" ]
}

@test "telemetryCollector/Deployment: replicas can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'telemetryCollector.replicas=3' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "3" ]
}

#--------------------------------------------------------------------
# Vault

@test "telemetryCollector/Deployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/telemetry-collector-deployment.yaml  \
    --set 'telemetryCollector.enabled=true' \
    --set 'telemetryCollector.image=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "telemetryCollector/Deployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/telemetry-collector-deployment.yaml  \
    --set 'telemetryCollector.enabled=true' \
    --set 'telemetryCollector.image=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "telemetryCollector/Deployment: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
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

@test "telemetryCollector/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set without vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
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

@test "telemetryCollector/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
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

@test "telemetryCollector/Deployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/telemetry-collector-deployment.yaml  \
    --set 'telemetryCollector.enabled=true' \
    --set 'telemetryCollector.image=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "telemetryCollector/Deployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/telemetry-collector-deployment.yaml  \
    --set 'telemetryCollector.enabled=true' \
    --set 'telemetryCollector.image=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

@test "telemetryCollector/Deployment: vault tls annotations are set when tls is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/cert/ca\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki_int/cert/ca" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-init-first"]' | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)"
  [ "${actual}" = "test" ]
}

@test "telemetryCollector/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# telemetryCollector.cloud

@test "telemetryCollector/Deployment: success with global.cloud env vars" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.resourceId.secretName=client-resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=client-resource-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-key' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_RESOURCE_ID")) | .[0].valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "client-resource-id-name" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_RESOURCE_ID")) | .[0].valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "client-resource-id-key" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_CLIENT_ID")) | .[0].valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "client-id-name" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_CLIENT_ID")) | .[0].valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "client-id-key" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_CLIENT_SECRET")) | .[0].valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "client-secret-name" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_CLIENT_SECRET")) | .[0].valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "client-secret-key" ]
}

@test "telemetryCollector/Deployment: success with telemetryCollector.cloud env vars" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'global.cloud.enabled=false' \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.cloud.resourceId.secretName=client-resource-id-name' \
      --set 'telemetryCollector.cloud.resourceId.secretKey=client-resource-id-key' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-name' \
      --set 'telemetryCollector.cloud.clientSecret.secretKey=client-secret-key' \
      --set 'telemetryCollector.cloud.clientId.secretName=client-id-name' \
      --set 'telemetryCollector.cloud.clientId.secretKey=client-id-key' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_RESOURCE_ID")) | .[0].valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "client-resource-id-name" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_RESOURCE_ID")) | .[0].valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "client-resource-id-key" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_CLIENT_ID")) | .[0].valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "client-id-name" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_CLIENT_ID")) | .[0].valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "client-id-key" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_CLIENT_SECRET")) | .[0].valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "client-secret-name" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_CLIENT_SECRET")) | .[0].valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "client-secret-key" ]
}

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.clientId is set and global.cloud.resourceId is not set or global.cloud.clientSecret.secretName is not set" {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientSecret.secretName=client-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=client-resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=client-resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "telemetryCollector/Deployment: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "telemetryCollector/Deployment: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "telemetryCollector/Deployment: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "telemetryCollector/Deployment: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "telemetryCollector/Deployment: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "telemetryCollector/Deployment: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "telemetryCollector/Deployment: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "telemetryCollector/Deployment: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "telemetryCollector/Deployment: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.clientId.secretName is set but telemetryCollector.cloud.clientId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.cloud.clientId.secretName=client-id-name' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'telemetryCollector.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]

  echo "$output" > /dev/stderr

  [[ "$output" =~ "When either telemetryCollector.cloud.clientId.secretName or telemetryCollector.cloud.clientId.secretKey is defined, both must be set." ]]
}

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.clientId.secretKey is set but telemetryCollector.cloud.clientId.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.cloud.clientId.secretName=client-id-name' \
      --set 'telemetryCollector.cloud.clientId.secretKey=client-id-key' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]

  echo "$output" > /dev/stderr

  [[ "$output" =~ "When either telemetryCollector.cloud.clientSecret.secretName or telemetryCollector.cloud.clientSecret.secretKey is defined, both must be set." ]]
}

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.clientSecret.secretName is set but telemetryCollector.cloud.clientId.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.cloud.clientId.secretKey=client-id-key' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'telemetryCollector.cloud.clientSecret.secretKey=client-secret-key-name'  \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]

  echo "$output" > /dev/stderr

  [[ "$output" =~ "When telemetryCollector.cloud.clientSecret.secretName is set, telemetryCollector.cloud.clientId.secretName must also be set." ]]
}

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.clientId.secretName is set but telemetry.cloud.clientId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.cloud.clientId.secretName=client-id-name' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-name' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]

  [[ "$output" =~ "When either telemetryCollector.cloud.clientId.secretName or telemetryCollector.cloud.clientId.secretKey is defined, both must be set." ]]
}

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.clientSecret.secretName is set but telemetry.cloud.clientSecret.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.cloud.clientId.secretName=client-id-name' \
      --set 'telemetryCollector.cloud.clientId.secretKey=client-id-key' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-name' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]

  [[ "$output" =~ "When either telemetryCollector.cloud.clientSecret.secretName or telemetryCollector.cloud.clientSecret.secretKey is defined, both must be set." ]]
}

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.clientId and telemetryCollector.cloud.clientSecret is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.cloud.clientId.secretName=client-id-name' \
      --set 'telemetryCollector.cloud.clientId.secretKey=client-id-key' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-name' \
      --set 'telemetryCollector.cloud.clientSecret.secretKey=client-secret-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      .
  [ "$status" -eq 1 ]

  [[ "$output" =~ "When telemetryCollector has clientId and clientSecret, telemetryCollector.cloud.resourceId.secretKey or global.cloud.resourceId.secretKey must be set" ]]
}

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.resourceId.secretName differs from global.cloud.resourceId.secretName." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.cloud.resourceId.secretName=resource-id-1' \
      --set 'global.cloud.resourceId.secretKey=key' \
      --set 'telemetryCollector.cloud.resourceId.secretName=resource-id-2' \
      --set 'telemetryCollector.cloud.resourceId.secretKey=key' \
      --set 'telemetryCollector.cloud.clientId.secretName=client-id-name' \
      --set 'telemetryCollector.cloud.clientId.secretKey=client-id-key' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-name' \
      --set 'telemetryCollector.cloud.clientSecret.secretKey=client-secret-name' \
      .
  [ "$status" -eq 1 ]

  [[ "$output" =~ "When both global.cloud.resourceId.secretName and telemetryCollector.cloud.resourceId.secretName are set, they should be the same." ]]
}

@test "telemetryCollector/Deployment: fails when telemetryCollector.cloud.resourceId.secretKey differs from global.cloud.resourceId.secretKey." {
  cd `chart_dir`
  run helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.cloud.resourceId.secretName=name' \
      --set 'global.cloud.resourceId.secretKey=key-1' \
      --set 'telemetryCollector.cloud.resourceId.secretName=name' \
      --set 'telemetryCollector.cloud.resourceId.secretKey=key-2' \
      --set 'telemetryCollector.cloud.clientId.secretName=client-id-name' \
      --set 'telemetryCollector.cloud.clientId.secretKey=client-id-key' \
      --set 'telemetryCollector.cloud.clientSecret.secretName=client-secret-name' \
      --set 'telemetryCollector.cloud.clientSecret.secretKey=client-secret-name' \
      .
  [ "$status" -eq 1 ]

  [[ "$output" =~ "When both global.cloud.resourceId.secretKey and telemetryCollector.cloud.resourceId.secretKey are set, they should be the same." ]]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "telemetryCollector/Deployment: sets -tls-disabled args when when not using TLS." {
  cd `chart_dir`

  local flags=$(helm template  \
    -s templates/telemetry-collector-deployment.yaml \
    --set 'telemetryCollector.enabled=true'     \
    --set 'telemetryCollector.image=bar'    \
    --set 'global.tls.enabled=false'    \
    . | yq -r .spec.template.spec.containers[1].args)

  local actual=$(echo $flags | yq -r '. | any(contains("-tls-disabled"))')
  [ "${actual}" = 'true' ]

}

@test "telemetryCollector/Deployment: -ca-certs set correctly when using TLS." {
  cd `chart_dir`
  local flags=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].args' | tee /dev/stderr)

  local actual=$(echo $flags | yq -r '. | any(contains("-ca-certs=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

#--------------------------------------------------------------------
# External Server

@test "telemetryCollector/Deployment: sets external server args when global.tls.enabled and externalServers.enabled" {
  cd `chart_dir`
  local flags=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.httpsPort=8501' \
      --set 'externalServers.tlsServerName=foo.tls.server' \
      --set 'externalServers.useSystemRoots=true' \
      --set 'server.enabled=false' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].args' | tee /dev/stderr)

  local actual=$(echo $flags | yq -r '. | any(contains("-ca-certs=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = 'false' ]

  local actual=$(echo $flags | yq -r '. | any(contains("-tls-server-name=foo.tls.server"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]

  local actual=$(echo $flags | jq -r '. | any(contains("-addresses=external-consul.host"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

#--------------------------------------------------------------------
# Admin Partitions

@test "telemetryCollector/Deployment: partition flags are set when using admin partitions" {
  cd `chart_dir`
  local flags=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=hashi' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[1].args' | tee /dev/stderr)

  local actual=$(echo $flags | jq -r '. | any(contains("-login-partition=hashi"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]

  local actual=$(echo $flags | jq -r '. | any(contains("-service-partition=hashi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Deployment: consul-ca-cert volume mount is not set when using externalServers and useSystemRoots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "telemetryCollector/Deployment: config volume mount is set when config exists" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.customExporterConfig="foo"' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "config") | .name' | tee /dev/stderr)
  [ "${actual}" = "config" ]
}

@test "telemetryCollector/Deployment: config flag is set when config exists" {
  cd `chart_dir`
  local flags=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'telemetryCollector.customExporterConfig="foo"' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command')

  local actual=$(echo $flags | yq -r '.  | any(contains("-config-file-path /consul/config/config.json"))')
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Deployment: consul-ca-cert volume mount is not set on acl-init when using externalServers and useSystemRoots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[1].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}
#--------------------------------------------------------------------
# trustedCAs

@test "telemetryCollector/Deployment: trustedCAs: if trustedCAs is set command is modified correctly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.trustedCAs[0]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2] | contains("cat <<EOF > /trusted-cas/custom-ca-0.pem")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Deployment: trustedCAs: if multiple Trusted cas were set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.trustedCAs[0]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      --set 'global.trustedCAs[1]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0]'  | tee /dev/stderr)


  local actual=$(echo $object | jq '.command[2] | contains("cat <<EOF > /trusted-cas/custom-ca-0.pem")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo $object | jq '.command[2] | contains("cat <<EOF > /trusted-cas/custom-ca-1.pem")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Deployment: trustedCAs: if trustedCAs is set /trusted-cas volumeMount is added" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.trustedCAs[0]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr | yq -r '.spec.template.spec' | tee /dev/stderr)
  local actual=$(echo $object | jq -r '.volumes[] | select(.name == "trusted-cas") | .name' | tee /dev/stderr)
  [ "${actual}" = "trusted-cas" ]
}


@test "telemetryCollector/Deployment: trustedCAs: if trustedCAs is set SSL_CERT_DIR env var is set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.trustedCAs[0]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr | yq -r '.spec.template.spec.containers[0].env[] | select(.name == "SSL_CERT_DIR")' | tee /dev/stderr)

  local actual=$(echo $object | jq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "SSL_CERT_DIR" ]
  local actual=$(echo $object | jq -r '.value' | tee /dev/stderr)
  [ "${actual}" = "/etc/ssl/certs:/trusted-cas" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "telemetryCollector/Deployment: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component") | del(."consul.hashicorp.com/connect-inject-managed-by")' \
      | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "telemetryCollector/Deployment: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "telemetryCollector/Deployment: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
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
# extraEnvironmentVariables

@test "telemetryCollector/Deployment: extra environment variables" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.extraEnvironmentVars.HCP_AUTH_TLS=insecure' \
      --set 'telemetryCollector.extraEnvironmentVars.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r 'map(select(.name == "HCP_AUTH_TLS")) | .[0].value' | tee /dev/stderr)
  [ "${actual}" = "insecure" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "foo")) | .[0].value' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# logLevel

@test "telemetryCollector/Deployment: use global.logLevel by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Deployment: override global.logLevel when telemetryCollector.logLevel is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.logLevel=warn' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=warn"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Deployment: use global.logLevel by default for dataplane container" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Deployment: override global.logLevel when telemetryCollector.logLevel is set for dataplane container" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/telemetry-collector-deployment.yaml \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.logLevel=debug' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.experiments=["resource-apis"]

@test "telemetryCollector/Deployment: disabled when V2 is enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'ui.enabled=false' \
      --set 'global.experiments[0]=resource-apis' \
      .
}

#--------------------------------------------------------------------
# Namespaces

@test "telemetryCollector/Deployment: namespace flags when mirroringK8S" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      --namespace 'test-namespace' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo $object | jq -r '.containers[1].args | any(contains("-login-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]

  local actual=$(echo $object | jq -r '.containers[1].args | any(contains("-service-namespace=test-namespace"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

@test "telemetryCollector/Deployment: namespace flags when not mirroringK8S" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.image=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=false' \
      --set 'connectInject.consulNamespaces.consulDestinationNamespace=fakenamespace' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers' | tee /dev/stderr)

  local actual=$(echo $object | jq -r '.[1].args | any(contains("-login-namespace=fakenamespace"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]

  local actual=$(echo $object | jq -r '.[1].args | any(contains("-service-namespace=fakenamespace"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

#--------------------------------------------------------------------
# global.metrics.datadog.otlp

@test "telemetryCollector/Deployment: DataDog OTLP Collector HTTP protocol verification" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.cloud.enabled=false' \
      --set 'global.metrics.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true' \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.otlp.enabled=true' \
      --set 'global.metrics.datadog.otlp.protocol'="http" \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.[] | select(.name=="CO_OTEL_HTTP_ENDPOINT").value' | tee /dev/stderr)
  [ "${actual}" = 'http://$(HOST_IP):4318' ]
}

@test "telemetryCollector/Deployment: DataDog OTLP Collector HTTP protocol verification, case-insensitive" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.cloud.enabled=false' \
      --set 'global.metrics.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true' \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.otlp.enabled=true' \
      --set 'global.metrics.datadog.otlp.protocol'="HTTP" \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.[] | select(.name=="CO_OTEL_HTTP_ENDPOINT").value' | tee /dev/stderr)
  [ "${actual}" = 'http://$(HOST_IP):4318' ]
}

@test "telemetryCollector/Deployment: DataDog OTLP Collector gRPC protocol verification" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.cloud.enabled=false' \
      --set 'global.metrics.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true' \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.otlp.enabled=true' \
      --set 'global.metrics.datadog.otlp.protocol'="grpc" \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.[] | select(.name=="CO_OTEL_HTTP_ENDPOINT").value' | tee /dev/stderr)
  [ "${actual}" = 'http://$(HOST_IP):4317' ]
}

@test "telemetryCollector/Deployment: DataDog OTLP Collector gRPC protocol verification, case-insensitive" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/telemetry-collector-deployment.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'telemetryCollector.cloud.enabled=false' \
      --set 'global.metrics.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true' \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.otlp.enabled=true' \
      --set 'global.metrics.datadog.otlp.protocol'="gRPC" \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.[] | select(.name=="CO_OTEL_HTTP_ENDPOINT").value' | tee /dev/stderr)
  [ "${actual}" = 'http://$(HOST_IP):4317' ]
}