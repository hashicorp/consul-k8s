#!/usr/bin/env bats

load _helpers

@test "apiGateway/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      .
}

@test "apiGateway/Deployment: fails if no image is set" {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "apiGateway.image must be set to enable api gateway" ]]
}

@test "apiGateway/Deployment: disable with apiGateway.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=false' \
      .
}

@test "apiGateway/Deployment: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "apiGateway/Deployment: enable namespaces" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("-consul-destination-namespace=default")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: enable namespace mirroring" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("-mirroring-k8s=true")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: enable namespace mirroring prefixes" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      --set 'connectInject.consulNamespaces.mirroringK8SPrefix=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("-mirroring-k8s-prefix=foo")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: container image overrides" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "\"bar\"" ]
}

@test "apiGateway/Deployment: SDS host set correctly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("-sds-server-host release-name-consul-api-gateway-controller.default.svc")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "apiGateway/Deployment: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "apiGateway/Deployment: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.controller.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "apiGateway/Deployment: Adds tls-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "apiGateway/Deployment: Adds tls-ca-cert volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "apiGateway/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: consul-auto-encrypt-ca-cert volume is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: consul-auto-encrypt-ca-cert volumeMount is added when TLS with auto-encrypt is enabled with clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: consul-ca-cert volumeMount is added when TLS with auto-encrypt is enabled without clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: get-auto-encrypt-client-ca init container is created when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: adds both init containers when TLS with auto-encrypt and ACLs + namespaces are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers | length == 3' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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
# global.acls.manageSystemACLs

@test "apiGateway/Deployment: consul-logout preStop hook is added when ACLs are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].lifecycle.preStop.exec.command[1]] | any(contains("logout"))' | tee /dev/stderr)
  [ "${object}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_HTTP_TOKEN_FILE is not set when acls are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[1].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "apiGateway/Deployment: CONSUL_HTTP_TOKEN_FILE is set when acls are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[1].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_LOGIN_DATACENTER is set when acls are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[2].name] | any(contains("CONSUL_LOGIN_DATACENTER"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: init container is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[1]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "api-gateway-controller-acl-init" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[0].name] | any(contains("NAMESPACE"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("POD_NAME"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].name] | any(contains("CONSUL_LOGIN_META"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].value] | any(contains("component=api-gateway-controller,pod=$(NAMESPACE)/$(POD_NAME)"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[3].name] | any(contains("CONSUL_LOGIN_DATACENTER"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '[.env[8].value] | any(contains("5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.consulAPITimeout=5s' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "api-gateway-controller-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "NAMESPACE") | [.valueFrom.fieldRef.fieldPath] | any(contains("metadata.namespace"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "POD_NAME") | [.valueFrom.fieldRef.fieldPath] | any(contains("metadata.name"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_LOGIN_META") | [.value] | any(contains("component=api-gateway-controller,pod=$(NAMESPACE)/$(POD_NAME)"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_ADDRESSES") | [.value] | any(contains("release-name-consul-server.default.svc"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_GRPC_PORT") | [.value] | any(contains("8502"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_HTTP_PORT") | [.value] | any(contains("8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_DATACENTER") | [.value] | any(contains("dc1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_API_TIMEOUT") | [.value] | any(contains("5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_USE_TLS") | [.value] | any(contains("true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_CACERT_FILE") | [.value] | any(contains("/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[] | select(.name == "consul-ca-cert") | [.mountPath] | any(contains("/consul/tls/ca"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[] | select(.name == "consul-data") | [.mountPath] | any(contains("/consul/login"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command with Partitions enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=default' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "api-gateway-controller-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-auth-method-name=release-name-consul-k8s-component-auth-method"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "NAMESPACE") | [.valueFrom.fieldRef.fieldPath] | any(contains("metadata.namespace"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "POD_NAME") | [.valueFrom.fieldRef.fieldPath] | any(contains("metadata.name"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_LOGIN_META") | [.value] | any(contains("component=api-gateway-controller,pod=$(NAMESPACE)/$(POD_NAME)"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_ADDRESSES") | [.value] | any(contains("release-name-consul-server.default.svc"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_GRPC_PORT") | [.value] | any(contains("8502"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_HTTP_PORT") | [.value] | any(contains("8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_DATACENTER") | [.value] | any(contains("dc1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_API_TIMEOUT") | [.value] | any(contains("5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_PARTITION") | [.value] | any(contains("default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_LOGIN_PARTITION") | [.value] | any(contains("default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_USE_TLS") | [.value] | any(contains("true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_CACERT_FILE") | [.value] | any(contains("/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[] | select(.name == "consul-ca-cert") | [.mountPath] | any(contains("/consul/tls/ca"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[] | select(.name == "consul-data") | [.mountPath] | any(contains("/consul/login"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: consul login datacenter is set to primary when when federation enabled in non-primary datacenter" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'meshGateway.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.datacenter=dc1' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc2' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[1]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq '[.env[3].name] | any(contains("CONSUL_LOGIN_DATACENTER"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[3].value] | any(contains("dc2"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: primary-datacenter flag provided when federation enabled in non-primary datacenter" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.datacenter=dc2' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name == "api-gateway-controller")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-api-gateway server"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-primary-datacenter=dc1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command when federation enabled in non-primary datacenter" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.datacenter=dc2' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "api-gateway-controller-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-auth-method-name=release-name-consul-k8s-component-auth-method-dc2"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[3].value] | any(contains("dc1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled and autoencrypt enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "api-gateway-controller-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "NAMESPACE") | [.valueFrom.fieldRef.fieldPath] | any(contains("metadata.namespace"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "POD_NAME") | [.valueFrom.fieldRef.fieldPath] | any(contains("metadata.name"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_LOGIN_META") | [.value] | any(contains("component=api-gateway-controller,pod=$(NAMESPACE)/$(POD_NAME)"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_ADDRESSES") | [.value] | any(contains("release-name-consul-server.default.svc"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_GRPC_PORT") | [.value] | any(contains("8502"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_HTTP_PORT") | [.value] | any(contains("8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_DATACENTER") | [.value] | any(contains("dc1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_API_TIMEOUT") | [.value] | any(contains("5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_USE_TLS") | [.value] | any(contains("true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.env[] | select(.name == "CONSUL_CACERT_FILE") | [.value] | any(contains("/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[] | select(.name == "consul-ca-cert") | [.mountPath] | any(contains("/consul/tls/ca"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[] | select(.name == "consul-data") | [.mountPath] | any(contains("/consul/login"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: init container for copy consul is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "copy-consul-bin")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("cp"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.volumeMounts[0] | any(contains("consul-bin"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: volumeMount for copy consul is created on container when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[0] | any(contains("consul-bin"))' | tee /dev/stderr)

  [ "${object}" = "true" ]
}

@test "apiGateway/Deployment: volume for copy consul is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[0] | any(contains("consul-bin"))' | tee /dev/stderr)

  [ "${object}" = "true" ]
}

@test "apiGateway/Deployment: auto-encrypt init container is created and is the first init-container when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled and autoencrypt enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[1]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "get-auto-encrypt-client-ca" ]
}

#--------------------------------------------------------------------
# resources

@test "apiGateway/Deployment: resources has default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "100Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "100m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "100Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "100m" ]
}

@test "apiGateway/Deployment: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.resources.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# init container resources

@test "apiGateway/Deployment: init container has default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "25Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "50m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "150Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "50m" ]
}

@test "apiGateway/Deployment: init container resources can be set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'apiGateway.initCopyConsulContainer.resources.requests.memory=memory' \
      --set 'apiGateway.initCopyConsulContainer.resources.requests.cpu=cpu' \
      --set 'apiGateway.initCopyConsulContainer.resources.limits.memory=memory2' \
      --set 'apiGateway.initCopyConsulContainer.resources.limits.cpu=cpu2' \
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

@test "apiGateway/Deployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "apiGateway/Deployment: can set a priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.controller.priorityClassName=name' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "name" ]
}

#--------------------------------------------------------------------
# logLevel

@test "apiGateway/Deployment: logLevel info by default from global" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: logLevel can be overridden" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.logLevel=debug' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# replicas

@test "apiGateway/Deployment: replicas defaults to 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "1" ]
}

@test "apiGateway/Deployment: replicas can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.controller.replicas=3' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "3" ]
}


#--------------------------------------------------------------------
# get-auto-encrypt-client-ca

@test "apiGateway/Deployment: get-auto-encrypt-client-ca uses server's stateful set address by default and passes ca cert" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/api-gateway-controller-deployment.yaml  \
    --set 'apiGateway.enabled=true' \
    --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/api-gateway-controller-deployment.yaml  \
    --set 'apiGateway.enabled=true' \
    --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/api-gateway-controller-deployment.yaml  \
    --set 'apiGateway.enabled=true' \
    --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/api-gateway-controller-deployment.yaml  \
    --set 'apiGateway.enabled=true' \
    --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: vault tls annotations are set when tls is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are set without vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/agent-extra-secret: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "vns" ]
}

@test "apiGateway/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.secretsBackend.vault.agentAnnotations="vault.hashicorp.com/namespace": bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "bar" ]
}

@test "apiGateway/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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
# global.cloud

@test "apiGateway/Deployment: fails when global.cloud.enabled is true and global.cloud.clientId.secretName is not set but global.cloud.clientSecret.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

@test "apiGateway/Deployment: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
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

#--------------------------------------------------------------------
# CONSUL_HTTP_SSL

@test "apiGateway/Deployment: CONSUL_HTTP_SSL set correctly when not using TLS." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[2].value' | tee /dev/stderr)
  [ "${actual}" = "\"false\"" ]
}

@test "apiGateway/Deployment: CONSUL_HTTP_SSL set correctly when using TLS." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[3].value' | tee /dev/stderr)
  [ "${actual}" = "\"true\"" ]
}

#--------------------------------------------------------------------
# CONSUL_HTTP_ADDR

@test "apiGateway/Deployment: CONSUL_HTTP_ADDR set correctly with external servers, TLS, and no clients." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.httpsPort=8501' \
      --set 'server.enabled=false' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[2].value] | any(contains("external-consul.host:8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_HTTP_ADDR set correctly with external servers, no TLS, and no clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.httpsPort=8500' \
      --set 'server.enabled=false' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[1].value] | any(contains("external-consul.host:8500"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_HTTP_ADDR set correctly with local servers, TLS, and clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[2].value] | any(contains("$(HOST_IP):8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_HTTP_ADDR set correctly with local servers, no TLS, and clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[1].value] | any(contains("$(HOST_IP):8500"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_HTTP_ADDR set correctly with local servers, TLS, and no clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[2].value] | any(contains("release-name-consul-server:8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_HTTP_ADDR set correctly with local servers, no TLS, and no clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=false' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[1].value] | any(contains("release-name-consul-server:8500"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# externalServers tlsServerName

@test "apiGateway/Deployment: CONSUL_TLS_SERVER_NAME can be set for externalServers" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.httpsPort=8501' \
      --set 'externalServers.tlsServerName=hashi' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[4].value == "hashi"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_TLS_SERVER_NAME will not be set for when clients are used" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.httpsPort=8501' \
      --set 'externalServers.tlsServerName=hashi' \
      --set 'client.enabled=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select (.name == "api-gateway-controller") | .env[] | select(.name == "CONSUL_TLS_SERVER_NAME")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# Admin Partitions

@test "apiGateway/Deployment: CONSUL_PARTITION is set when using admin partitions" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=hashi' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[3].value == "hashi"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_LOGIN_PARTITION is set when using admin partitions with ACLs" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=hashi' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[6].value == "hashi"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_DYNAMIC_SERVER_DISCOVERY is set when not using clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[3].value == "true"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_DYNAMIC_SERVER_DISCOVERY is not set when using clients" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[3]' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "apiGateway/Deployment: CONSUL_CACERT is set when using tls and clients even when useSystemRoots is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[0].name == "CONSUL_CACERT"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_CACERT is set when using tls and internal servers" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[0].name == "CONSUL_CACERT"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_CACERT has correct path with Vault as secrets backend and client disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'server.enabled=true' \
      --set 'client.enabled=false' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      . | tee /dev/stderr|
      yq '.spec.template.spec.containers[0].env[0].value == "/vault/secrets/serverca.crt"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Deployment: CONSUL_CACERT is not set when using tls and useSystemRoots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[0].name == "CONSUL_CACERT"' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "apiGateway/Deployment: consul-ca-cert volume mount is not set when using externalServers and useSystemRoots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
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

@test "apiGateway/Deployment: consul-ca-cert volume mount is not set when using Vault as a secrets backend" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "apiGateway/Deployment: consul-ca-cert volume mount is not set on acl-init when using externalServers and useSystemRoots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
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

@test "apiGateway/Deployment: consul-ca-cert volume mount is not set on acl-init when using Vault as secrets backend" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[1].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "apiGateway/Deployment: consul-auto-encrypt-ca-cert volume mount is set when tls.enabled, client.enabled, externalServers, useSystemRoots, and autoencrypt" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'client.enabled=true' \
      --set 'server.enabled=false' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'externalServers.hosts[0]=external-consul.host' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-auto-encrypt-ca-cert") | .mountPath' | tee /dev/stderr)
  [ "${actual}" = '"/consul/tls/ca"' ]
}

#--------------------------------------------------------------------
# extraLabels

@test "apiGateway/Deployment: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "apiGateway/Deployment: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "apiGateway/Deployment: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=bar' \
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
