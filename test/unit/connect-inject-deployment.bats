#!/usr/bin/env bats

load _helpers

@test "connectInject/Deployment: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: enable with global.enabled false, client.enabled true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: disable with connectInject.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: fails if global.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if global.enabled=true and client.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=true' \
      --set 'client.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if global.enabled=false and client.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if client.grpc=false" {
  cd `chart_dir`
  run helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'client.grpc=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "client.grpc must be true for connect injection" ]]
}

#--------------------------------------------------------------------
# consul and envoy images

@test "connectInject/Deployment: container image is global default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.imageK8S=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "\"foo\"" ]
}

@test "connectInject/Deployment: container image overrides" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.imageK8S=foo' \
      --set 'connectInject.image=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "\"bar\"" ]
}

@test "connectInject/Deployment: consul-image defaults to global" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.image=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-image=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul-image can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.image=foo' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.imageConsul=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-image=\"bar\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: envoy-image is not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-envoy-image"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: envoy-image can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.imageEnvoy=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-envoy-image=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# cert secrets

@test "connectInject/Deployment: no secretName: no tls-{cert,key}-file set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-cert-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-key-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-auto"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: with secretName: tls-{cert,key}-file set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.certs.secretName=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-cert-file"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.certs.secretName=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-key-file"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.certs.secretName=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-auto"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}


#--------------------------------------------------------------------
# service account name

@test "connectInject/Deployment: with secretName: no serviceAccountName set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.certs.secretName=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.serviceAccountName | has("serviceAccountName")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: no secretName: serviceAccountName set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.serviceAccountName | contains("connect-injector-webhook-svc-account")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "connectInject/Deployment: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "connectInject/Deployment: nodeSelector is not set by default with sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "connectInject/Deployment: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# centralConfig

@test "connectInject/Deployment: centralConfig is enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-central-config"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: centralConfig can be disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-central-config"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: defaultProtocol is disabled by default with centralConfig enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-default-protocol"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: defaultProtocol can be enabled with centralConfig enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      --set 'connectInject.centralConfig.defaultProtocol=grpc' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-default-protocol=\"grpc\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# authMethod

@test "connectInject/Deployment: -acl-auth-method is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method="))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: -acl-auth-method is set when global.acls.manageSystemACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"release-name-consul-k8s-auth-method\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: -acl-auth-method is set to connectInject.overrideAuthMethodName" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.overrideAuthMethodName=override' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"override\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: -acl-auth-method is overridden by connectInject.overrideAuthMethodName if global.acls.manageSystemACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.overrideAuthMethodName=override' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"override\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "connectInject/Deployment: Adds tls-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "connectInject/Deployment: Adds both tls-ca-cert and certs volumes when global.tls.enabled is true and connectInject.certs.secretName is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "connectInject/Deployment: Adds tls-ca-cert volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "connectInject/Deployment: Adds both tls-ca-cert and certs volumeMounts when global.tls.enabled is true and connectInject.certs.secretName is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "connectInject/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: consul-auto-encrypt-ca-cert volume is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul-auto-encrypt-ca-cert volumeMount is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: get-auto-encrypt-client-ca init container is created when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: adds both init containers when TLS with auto-encrypt and ACLs + namespaces are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers | length == 2' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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
# k8sAllowNamespaces & k8sDenyNamespaces

@test "connectInject/Deployment: default is allow '*', deny nothing" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'map(select(test("allow-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'any(contains("allow-k8s-namespace=\"*\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'map(select(test("deny-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "connectInject/Deployment: can set allow and deny" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.k8sAllowNamespaces[0]=allowNamespace' \
      --set 'connectInject.k8sDenyNamespaces[0]=denyNamespace' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'map(select(test("allow-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'map(select(test("deny-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'any(contains("allow-k8s-namespace=\"allowNamespace\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("deny-k8s-namespace=\"denyNamespace\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# namespaces

@test "connectInject/Deployment: namespace options disabled by default" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: namespace options set with .global.enableConsulNamespaces=true" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: mirroring options set with .connectInject.consulNamespaces.mirroringK8S=true" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: prefix can be set with .connectInject.consulNamespaces.mirroringK8SPrefix" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

#--------------------------------------------------------------------
# namespaces + acl token

@test "connectInject/Deployment: aclInjectToken disabled when namespaces not enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.aclInjectToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: aclInjectToken disabled when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.aclInjectToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: aclInjectToken disabled when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.aclInjectToken.secretName=foo' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: aclInjectToken enabled when secretName and secretKey is provided" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.aclInjectToken.secretName=foo' \
      --set 'connectInject.aclInjectToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name]' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'map(select(test("CONSUL_HTTP_TOKEN"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# namespaces + global.acls.manageSystemACLs

@test "connectInject/Deployment: CONSUL_HTTP_TOKEN env variable created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] ' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'map(select(test("CONSUL_HTTP_TOKEN"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/Deployment: init container is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "injector-acl-init" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: cross namespace policy is not added when global.acls.manageSystemACLs=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-cross-namespace-acl-policy"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: cross namespace policy is added when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-cross-namespace-acl-policy"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# namespaces + http address

@test "connectInject/Deployment: CONSUL_HTTP_ADDR env variable not set when namespaces are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: CONSUL_HTTP_ADDR env variable set when namespaces are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: CONSUL_HTTP_ADDR and CONSUL_CACERT env variables set when namespaces are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] ' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("CONSUL_CACERT"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# namespaces + host ip

@test "connectInject/Deployment: HOST_IP env variable not set when namespaces are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("HOST_IP"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: HOST_IP env variable set when namespaces are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("HOST_IP"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
