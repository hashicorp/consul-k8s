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

@test "apiGateway/Deployment: consul-auto-encrypt-ca-cert volumeMount is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
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
      yq '[.spec.template.spec.containers[0].lifecycle.preStop.exec.command[2]] | any(contains("consul logout"))' | tee /dev/stderr)
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
      yq '[.env[1].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].value] | any(contains("http://$(HOST_IP):8500"))' | tee /dev/stderr)
      echo $actual
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
      yq -r '.command | any(contains("-acl-auth-method=release-name-consul-k8s-component-auth-method-dc2"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-primary-datacenter=dc1"))' | tee /dev/stderr)
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
