#!/usr/bin/env bats

load _helpers

@test "createFederationSecret/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/create-federation-secret-job.yaml  \
      .
}

@test "createFederationSecret/Job: fails when global.federation.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.createFederationSecret=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.federation.enabled must be true when global.federation.createFederationSecret is true" ]]
}

# NOTE: This error actually comes from server-statefulset but we test it here
# too because this job requires TLS to be enabled.
@test "createFederationSecret/Job: fails when global.tls.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.createFederationSecret=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.federation.enabled is true, global.tls.enabled must be true because federation is only supported with TLS enabled" ]]
}

# NOTE: This error actually comes from server-acl-init but we test it here
# too because this job requires that ACLs are enabled when createReplicationToken is true.
@test "createFederationSecret/Job: fails when global.acls.createReplicationToken is true but global.acls.manageSystemACLs is false" {
  cd `chart_dir`
  run helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.createReplicationToken=true' \
      --set 'global.federation.createFederationSecret=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "if global.acls.createReplicationToken is true, global.acls.manageSystemACLs must be true" ]]
}

@test "createFederationSecret/Job: fails when global.acls.createReplicationToken is false but global.acls.manageSystemACLs is true" {
  cd `chart_dir`
  run helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.createReplicationToken=false' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.federation.createFederationSecret=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.acls.createReplicationToken must be true when global.acls.manageSystemACLs is true because the federation secret must include the replication token" ]]
}

@test "createFederationSecret/Job: disabled by updatepartition != 0" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.acls.createReplicationToken=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'server.updatePartition=1' \
      .
}

@test "createFederationSecret/Job: mounts auto-created ca secrets by default" {
  cd `chart_dir`
  local volumes=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes' | tee /dev/stderr )

  local actual

  # test it uses the auto-generated ca secret
  actual=$(echo $volumes | yq 'map(select(.name=="consul-ca-cert" and .secret.secretName=="release-name-consul-ca-cert")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it uses the correct secret key for the auto-generated ca secret
  actual=$(echo $volumes | yq 'map(select(.name=="consul-ca-cert" and .secret.items[0].key =="tls.crt")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it uses the auto-generated ca key secret
  actual=$(echo $volumes | yq 'map(select(.name=="consul-ca-key" and .secret.secretName=="release-name-consul-ca-key")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it uses the correct secret key for the auto-generated ca key secret
  actual=$(echo $volumes | yq 'map(select(.name=="consul-ca-key" and .secret.items[0].key =="tls.key")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "createFederationSecet/Job: sets -consul-api-timeout" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.consulServiceName=my-service-name' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr | yq '.spec.template.spec.containers[0].command | any(contains("-consul-api-timeout=5s"))')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls

@test "createFederationSecret/Job: mounts caCert secrets when set manually" {
  cd `chart_dir`
  local volumes=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=custom-ca-cert' \
      --set 'global.tls.caCert.secretKey=customKey' \
      --set 'global.tls.caKey.secretName=custom-ca-key' \
      --set 'global.tls.caKey.secretKey=customKey2' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes' | tee /dev/stderr )

  local actual

  # test it uses the custom ca cert secret
  actual=$(echo $volumes | yq 'map(select(.name=="consul-ca-cert" and .secret.secretName=="custom-ca-cert")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it uses the custom ca cert secret key
  actual=$(echo $volumes | yq 'map(select(.name=="consul-ca-cert" and .secret.items[0].key =="customKey")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it uses the custom ca key secret
  actual=$(echo $volumes | yq 'map(select(.name=="consul-ca-key" and .secret.secretName=="custom-ca-key")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it uses the custom ca key secret key
  actual=$(echo $volumes | yq 'map(select(.name=="consul-ca-key" and .secret.items[0].key =="customKey2")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "createFederationSecret/Job: auto-encrypt disabled" {
  cd `chart_dir`
  local obj=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr)

  local actual

  # test it doesn't have any init containers
  actual=$(echo "$obj" | yq '.spec.template.spec.initContainers | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it sets CONSUL_CACERT to the server ca cert
  actual=$(echo "$obj" | yq '.spec.template.spec.containers[0].env | map(select(.name == "CONSUL_CACERT" and .value == "/consul/tls/ca/tls.crt")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.gossipEncryption

@test "createFederationSecret/Job: gossip encryption key set" {
  cd `chart_dir`
  local obj=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.gossipEncryption.secretName=gossip-secret' \
      --set 'global.gossipEncryption.secretKey=key' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr)

  local actual

  # test it mounts the secret
  actual=$(echo "$obj" | yq '.spec.template.spec.volumes | map(select(.name == "gossip-encryption-key" and .secret.secretName == "gossip-secret" and .secret.items[0].key == "key")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it sets the -gossip-key-file flag
  actual=$(echo "$obj" | yq '.spec.template.spec.containers[0].command | any(contains("-gossip-key-file"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "createFederationSecret/Job: gossip encryption key autogenerated" {
  cd `chart_dir`
  local obj=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr)
    
  local actual


  # test it mounts the secret
  actual=$(echo "$obj" | yq '.spec.template.spec.volumes | map(select(.name == "gossip-encryption-key" and .secret.secretName == "release-name-consul-gossip-encryption-key" and .secret.items[0].key == "key")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it sets the -gossip-key-file flag
  actual=$(echo "$obj" | yq '.spec.template.spec.containers[0].command | any(contains("-gossip-key-file"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.createReplicationToken

@test "createFederationSecret/Job: global.acls.createReplicationToken=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.createReplicationToken=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr | yq '.spec.template.spec.containers[0].command | any(contains("-export-replication-token=true"))')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# meshGateway.consulServiceName

@test "createFederationSecret/Job: sets -mesh-gateway-service-name to meshGateway.consulServiceName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.consulServiceName=my-service-name' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr | yq '.spec.template.spec.containers[0].command | any(contains("-mesh-gateway-service-name=my-service-name"))')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# tolerations

@test "createFederationSecret/Job: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .tolerations? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "createFederationSecret/Job: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      --set 'client.tolerations=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "createFederationSecret/Job: priorityClassName is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "createFederationSecret/Job: specified priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      --set 'client.priorityClassName=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "createFederationSecret/Job: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "createFederationSecret/Job: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      --set 'client.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "createFederationSecret/Job: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "createFederationSecret/Job: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "createFederationSecret/Job: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-job.yaml \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
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

@test "createFederationSecret/Job: logLevel is not set by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "createFederationSecret/Job: override the global.logLevel flag with global.federation.logLevel" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.federation.logLevel=debug' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
