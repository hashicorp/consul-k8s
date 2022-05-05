#!/usr/bin/env bats

load _helpers

@test "controller/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-deployment.yaml  \
      .
}

@test "controller/Deployment: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: command defaults" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("consul-k8s-control-plane controller"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# replicas

@test "controller/Deployment: replicas defaults to 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "controller/Deployment: can set replicas" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'controller.replicas=2' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "controller/Deployment: consul-logout preStop hook is added when ACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].lifecycle.preStop.exec.command[2]] | any(contains("consul-k8s-control-plane consul-logout -consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: CONSUL_HTTP_TOKEN_FILE is not set when acls are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[0].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "controller/Deployment: CONSUL_HTTP_TOKEN_FILE is set when acls are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[0].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls disabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "controller-acl-init" ]

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

@test "controller/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "controller-acl-init")' | tee /dev/stderr)

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

@test "controller/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command with Partitions enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=default' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "controller-acl-init")' | tee /dev/stderr)

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

@test "controller/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled and autoencrypt enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "controller-acl-init")' | tee /dev/stderr)

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

@test "controller/Deployment: auto-encrypt init container is created and is the first init-container when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled and autoencrypt enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "get-auto-encrypt-client-ca" ]
}

@test "controller/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command when federation enabled in non-primary datacenter" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.datacenter=dc2' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "controller-acl-init")' | tee /dev/stderr)

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

#--------------------------------------------------------------------
# global.tls.enabled

@test "controller/Deployment: Adds tls-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "controller/Deployment: Adds tls-ca-cert volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "controller/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
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

@test "controller/Deployment: consul-auto-encrypt-ca-cert volume is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: consul-auto-encrypt-ca-cert volumeMount is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: get-auto-encrypt-client-ca init container is created when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: adds both init containers when TLS with auto-encrypt and ACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers | length == 2' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
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
# partitions

@test "controller/Deployment: partitions options disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("partition"))' | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "controller/Deployment: partition name set with .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("partition=default"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "controller/Deployment: fails if namespaces are disabled and .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  run helm template \
      -s templates/controller-deployment.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=false' \
      --set 'controller.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.enableConsulNamespaces must be true if global.adminPartitions.enabled=true" ]]
}
#--------------------------------------------------------------------
# namespaces

@test "controller/Deployment: namespace options disabled by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "controller/Deployment: namespace options set with .global.enableConsulNamespaces=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "controller/Deployment: mirroring options set with connectInject.consulNamespaces.mirroringK8S=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "controller/Deployment: prefix can be set with connectInject.consulNamespaces.mirroringK8SPrefix" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      --set 'connectInject.consulNamespaces.mirroringK8SPrefix=k8s-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix=k8s-"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: cross namespace policy is not added when global.acls.manageSystemACLs=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-cross-namespace-acl-policy"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "controller/Deployment: cross namespace policy is added when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-cross-namespace-acl-policy"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# affinity

@test "controller/Deployment: affinity not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.affinity == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: affinity can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'controller.affinity=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .affinity == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "controller/Deployment: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "controller/Deployment: nodeSelector can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'controller.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# tolerations

@test "controller/Deployment: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'controller.tolerations=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .tolerations == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "controller/Deployment: priorityClassName not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: priorityClassName can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'controller.priorityClassName=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .priorityClassName == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# resources

@test "controller/Deployment: default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"100m","memory":"50Mi"},"requests":{"cpu":"100m","memory":"50Mi"}}' ]
}

@test "controller/Deployment: can set resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'controller.resources.requests.memory=100Mi' \
      --set 'controller.resources.requests.cpu=100m' \
      --set 'controller.resources.limits.memory=200Mi' \
      --set 'controller.resources.limits.cpu=200m' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"200m","memory":"200Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}' ]
}

#--------------------------------------------------------------------
# aclToken

@test "controller/Deployment: aclToken enabled when secretName and secretKey is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'controller.aclToken.secretName=foo' \
      --set 'controller.aclToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: aclToken env is set when ACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: aclToken env is not set when ACLs are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# logLevel

@test "controller/Deployment: logLevel info by default from global" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq '.containers[0].command | any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq '.initContainers[0].command | any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: logLevel can be overridden" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'controller.logLevel=error' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq '.containers[0].command | any(contains("-log-level=error"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq '.initContainers[0].command | any(contains("-log-level=error"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# get-auto-encrypt-client-ca

@test "controller/Deployment: get-auto-encrypt-client-ca uses server's stateful set address by default and passes ca cert" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
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

@test "controller/Deployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/controller-deployment.yaml  \
    --set 'controller.enabled=true' \
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

@test "controller/Deployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/controller-deployment.yaml  \
    --set 'controller.enabled=true' \
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

@test "controller/Deployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/controller-deployment.yaml  \
    --set 'controller.enabled=true' \
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

@test "controller/Deployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/controller-deployment.yaml  \
    --set 'controller.enabled=true' \
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

@test "controller/Deployment: vault tls annotations are set when tls is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enabled=true' \
      --set 'controller.tlsCert.secretName=pki/issue/controller-webhook-cert-dc1' \
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

    local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-tls.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki/issue/controller-webhook-cert-dc1" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-tls.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki/issue/controller-webhook-cert-dc1\" \"common_name=controller-webhook.dc1.consul\"\n\"alt_names=localhost,release-name-consul-controller-webhook,*.release-name-consul-controller-webhook,*.release-name-consul-controller-webhook.default,release-name-consul-controller-webhook.default,*.release-name-consul-controller-webhook.default.svc,release-name-consul-controller-webhook.default.svc,*.controller-webhook.dc1.consul\" \"ip_sans=127.0.0.1\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-tls.key"]' | tee /dev/stderr)"
  [ "${actual}" = "pki/issue/controller-webhook-cert-dc1" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-tls.key"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki/issue/controller-webhook-cert-dc1\" \"common_name=controller-webhook.dc1.consul\"\n\"alt_names=localhost,release-name-consul-controller-webhook,*.release-name-consul-controller-webhook,*.release-name-consul-controller-webhook.default,release-name-consul-controller-webhook.default,*.release-name-consul-controller-webhook.default.svc,release-name-consul-controller-webhook.default.svc,*.controller-webhook.dc1.consul\" \"ip_sans=127.0.0.1\" -}}\n{{- .Data.private_key -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "controller/Deployment: vault does not add cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'controller.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "cert")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "controller/Deployment: vault does not add cert volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'controller.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "cert")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "controller/Deployment: vault webhook-tls-cert-dir flag is set to /vault/secrets" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-webhook-tls-cert-dir=/vault/secrets"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault agent annotations

@test "controller/Deployment: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations | del(."consul.hashicorp.com/connect-inject") | del(."vault.hashicorp.com/agent-inject") | del(."vault.hashicorp.com/role")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "controller/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
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


