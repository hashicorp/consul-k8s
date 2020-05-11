#!/usr/bin/env bats

load _helpers

@test "createFederationSecet/Job: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-job.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "createFederationSecet/Job: fails when global.federation.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.createFederationSecret=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.federation.enabled must be true when global.federation.createFederationSecret is true" ]]
}

# NOTE: This error actually comes from server-statefulset but we test it here
# too because this job requires TLS to be enabled.
@test "createFederationSecet/Job: fails when global.tls.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.createFederationSecret=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.federation.enabled is true, global.tls.enabled must be true because federation is only supported with TLS enabled" ]]
}

# NOTE: This error actually comes from server-acl-init but we test it here
# too because this job requires that ACLs are enabled when createReplicationToken is true.
@test "createFederationSecet/Job: fails when global.acls.createReplicationToken is true but global.acls.manageSystemACLs is false" {
  cd `chart_dir`
  run helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.createReplicationToken=true' \
      --set 'global.federation.createFederationSecret=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "if global.acls.createReplicationToken is true, global.acls.manageSystemACLs must be true" ]]
}

@test "createFederationSecet/Job: fails when global.acls.createReplicationToken is false but global.acls.manageSystemACLs is true" {
  cd `chart_dir`
  run helm template \
      -x templates/create-federation-secret-job.yaml  \
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

@test "createFederationSecet/Job: mounts auto-created ca secrets by default" {
  cd `chart_dir`
  local volumes=$(helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' . \
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

#--------------------------------------------------------------------
# global.tls

@test "createFederationSecet/Job: mounts caCert secrets when set manually" {
  cd `chart_dir`
  local volumes=$(helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=custom-ca-cert' \
      --set 'global.tls.caCert.secretKey=customKey' \
      --set 'global.tls.caKey.secretName=custom-ca-key' \
      --set 'global.tls.caKey.secretKey=customKey2' \
      --set 'global.federation.createFederationSecret=true' . \
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

@test "createFederationSecet/Job: auto-encrypt disabled" {
  cd `chart_dir`
  local obj=$(helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.createFederationSecret=true' . \
      . | tee /dev/stderr)

  local actual

  # test it doesn't have any init containers
  actual=$(echo "$obj" | yq '.spec.template.spec.initContainers | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it sets CONSUL_CACERT to the server ca cert
  actual=$(echo "$obj" | yq '.spec.template.spec.containers[0].env | map(select(.name == "CONSUL_CACERT" and .value == "/consul/tls/ca/tls.crt")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "createFederationSecet/Job: auto-encrypt enabled" {
  cd `chart_dir`
  local obj=$(helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.federation.createFederationSecret=true' . \
      . | tee /dev/stderr)

  local actual

  # test it has the auto-encrypt volume
  actual=$(echo "$obj" | yq '.spec.template.spec.volumes | map(select(.name == "consul-auto-encrypt-ca-cert")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it adds the init container
  actual=$(echo "$obj" | yq '.spec.template.spec.initContainers | map(select(.name == "get-auto-encrypt-client-ca")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it sets CONSUL_CACERT to the auto-encrypt ca cert
  actual=$(echo "$obj" | yq '.spec.template.spec.containers[0].env | map(select(.name == "CONSUL_CACERT" and .value == "/consul/tls/client/ca/tls.crt")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.gossipEncryption

@test "createFederationSecet/Job: gossip encryption key set" {
  cd `chart_dir`
  local obj=$(helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.gossipEncryption.secretName=gossip-secret' \
      --set 'global.gossipEncryption.secretKey=key' \
      --set 'global.federation.createFederationSecret=true' . \
      . | tee /dev/stderr)

  local actual

  # test it mounts the secret
  actual=$(echo "$obj" | yq '.spec.template.spec.volumes | map(select(.name == "gossip-encryption-key" and .secret.secretName == "gossip-secret" and .secret.items[0].key == "key")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # test it sets the -gossip-key-file flag
  actual=$(echo "$obj" | yq '.spec.template.spec.containers[0].command | any(contains("-gossip-key-file"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.createReplicationToken

@test "createFederationSecet/Job: global.acls.createReplicationToken=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.createReplicationToken=true' \
      --set 'global.federation.createFederationSecret=true' . \
      . | tee /dev/stderr | yq '.spec.template.spec.containers[0].command | any(contains("-export-replication-token=true"))')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# meshGateway.consulServiceName

@test "createFederationSecet/Job: sets -mesh-gateway-service-name to meshGateway.consulServiceName" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-job.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.consulServiceName=my-service-name' \
      --set 'global.federation.createFederationSecret=true' . \
      . | tee /dev/stderr | yq '.spec.template.spec.containers[0].command | any(contains("-mesh-gateway-service-name=my-service-name"))')
  [ "${actual}" = "true" ]
}
