#!/usr/bin/env bats

load _helpers

@test "terminatingGateways/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/terminating-gateways-deployment.yaml \
      .
}

@test "terminatingGateways/Deployment: enabled with terminatingGateways, connectInject and client.grpc enabled, has default gateway name" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s '.[0]' | tee /dev/stderr)

  local actual=$(echo $object | yq '. | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object | yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-terminating-gateway" ]
}

@test "terminatingGateways/Deployment: Adds consul service volumeMount to gateway container" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | yq '.spec.template.spec.containers[0].volumeMounts[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "consul-service" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/service" ]

  local actual=$(echo $object |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "terminatingGateways/Deployment: Connect init uses consulAPITimeout default." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.enabled=true' \
      . | yq '.spec.template.spec.initContainers[0].command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# prerequisites

@test "terminatingGateways/Deployment: fails if connectInject.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=false' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.enabled must be true" ]]
}

@test "terminatingGateways/Deployment: fails if there are duplicate gateway names" {
  cd `chart_dir`
  run helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.gateways[0].name=foo' \
      --set 'terminatingGateways.gateways[1].name=foo' \
      --set 'connectInject.enabled=true' \
      --set 'global.enabled=true' \
      --set 'client.enabled=true' .
  echo "status: $output"
  [ "$status" -eq 1 ]
  [[ "$output" =~ "terminating gateways must have unique names but found duplicate name foo" ]]
}

@test "terminatingGateways/Deployment: fails if a terminating gateway has the same name as an ingress gateway" {
  cd `chart_dir`
  run helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'terminatingGateways.gateways[0].name=foo' \
      --set 'ingressGateways.gateways[0].name=foo' \
      --set 'connectInject.enabled=true' \
      --set 'global.enabled=true' \
      --set 'client.enabled=true' .
  echo "status: $output"
  [ "$status" -eq 1 ]
  [[ "$output" =~ "terminating gateways cannot have duplicate names of any ingress gateways but found duplicate name foo" ]]
}

#--------------------------------------------------------------------
# envoyImage

@test "terminatingGateways/Deployment: envoy image has default global value" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].image' | tee /dev/stderr)
  [[ "${actual}" =~ "envoyproxy/envoy:v" ]]
}

@test "terminatingGateways/Deployment: envoy image can be set using the global value" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.imageEnvoy=new/image' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "new/image" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "terminatingGateways/Deployment: sets TLS env variables for terminating-gateway-init when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'https://release-name-consul-server.default.svc:8501' ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "terminatingGateways/Deployment: sets TLS env variables for terminating-gateway when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'https://release-name-consul-server.default.svc:8501' ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_GRPC_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'https://release-name-consul-server.default.svc:8502' ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "terminatingGateways/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo-ca-cert' \
      --set 'global.tls.caCert.secretKey=key' \
      --set 'global.tls.caKey.secretName=foo-ca-key' \
      --set 'global.tls.caKey.secretKey=key' \
      . | tee /dev/stderr |
      yq -s '.[0].spec.template.spec.volumes[] | select(.name=="consul-ca-cert")' | tee /dev/stderr)

  # check that the provided ca cert secret is attached as a volume
  local actual=$(echo $ca_cert_volume | yq -r '.secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-cert" ]

  # check that the volume uses the provided secret key
  local actual=$(echo $ca_cert_volume | yq -r '.secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]
}

@test "terminatingGateways/Deployment: CA cert volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr )
  [ "${actual}" != "" ]
}

@test "terminatingGateways/Deployment: CA cert volume omitted when TLS is enabled with external servers and use system roots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      . | yq '.[0]spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr )
  [ "${actual}" == "" ]
}

@test "terminatingGateways/Deployment: serviceAccountName is set properly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'terminatingGateways.defaults.consulNamespace=namespace' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.serviceAccountName' | tee /dev/stderr)

  [ "${actual}" = "release-name-consul-terminating-gateway" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

# TODO re-add this coverage with correct settings when dataplane is integrated
# @test "terminatingGateways/Deployment: consul-sidecar uses -token-file flag when global.acls.manageSystemACLs=true" {
#   cd `chart_dir`
#   local actual=$(helm template \
#       -s templates/terminating-gateways-deployment.yaml \
#       --set 'terminatingGateways.enabled=true' \
#       --set 'connectInject.enabled=true' \
#       --set 'global.acls.manageSystemACLs=true' \
#       . | tee /dev/stderr |
#       yq -s '.[0].spec.template.spec.containers[0].command | any(contains("-token-file=/consul/service/acl-token"))' | tee /dev/stderr)
#   [ "${actual}" = "true" ]
# }

@test "terminatingGateways/Deployment: Adds consul envvars CONSUL_HTTP_ADDR on terminating-gateway-init init container when ACLs are enabled and tls is enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "https://release-name-consul-server.default.svc:8501" ]
}

@test "terminatingGateways/Deployment: Adds consul envvars CONSUL_HTTP_ADDR on terminating-gateway-init init container when ACLs are enabled and tls is not enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "http://release-name-consul-server.default.svc:8500" ]
}

@test "terminatingGateways/Deployment: Does not add consul envvars CONSUL_CACERT on terminating-gateway-init init container when ACLs are enabled and tls is not enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0].env[] | select(.name == "CONSUL_CACERT")' | tee /dev/stderr)

  [ "${actual}" = "" ]
}

@test "terminatingGateways/Deployment: Adds consul envvars CONSUL_CACERT on terminating-gateway-init init container when ACLs are enabled and tls is enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
    [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "terminatingGateways/Deployment: CONSUL_HTTP_TOKEN_FILE is not set when acls are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'global.acls.manageSystemACLs=false' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[0].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "terminatingGateways/Deployment: CONSUL_HTTP_TOKEN_FILE is set when acls are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s '[.[0].spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

# TODO re-add this coverage with correct settings when dataplane is integrated
# @test "terminatingGateways/Deployment: consul-logout preStop hook is added when ACLs are enabled" {
#   cd `chart_dir`
#   local actual=$(helm template \
#       -s templates/terminating-gateways-deployment.yaml  \
#       --set 'terminatingGateways.enabled=true' \
#       --set 'connectInject.enabled=true' \
#       --set 'terminatingGateways.enabled=true' \
#       --set 'global.acls.manageSystemACLs=true' \
#       . | tee /dev/stderr |
#       yq '[.spec.template.spec.containers[0].lifecycle.preStop.exec.command[3]] | any(contains("/consul-bin/consul logout"))' | tee /dev/stderr)
#   [ "${actual}" = "true" ]
# }

#--------------------------------------------------------------------
# metrics

@test "terminatingGateways/Deployment: when global.metrics.enabled=true, adds prometheus scrape=true annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
       yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/scrape"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "terminatingGateways/Deployment: when global.metrics.enabled=true, adds prometheus port=20200 annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
       yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/port"' | tee /dev/stderr)
  [ "${actual}" = "20200" ]
}

@test "terminatingGateways/Deployment: when global.metrics.enabled=true, adds prometheus path=/metrics annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
       yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/metrics" ]
}

@test "terminatingGateways/Deployment: when global.metrics.enableGatewayMetrics=false, does not set prometheus annotations" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableGatewayMetrics=false'  \
      . | tee /dev/stderr |
      yq '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -s -r '.[0].metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo $object | yq -s -r '.[0].metadata.annotations."prometheus.io/port"' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo $object | yq -s -r '.[0].metadata.annotations."prometheus.io/scrape"' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "terminatingGateways/Deployment: when global.metrics.enabled=false, does not set prometheus annotations" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=false'  \
      . | tee /dev/stderr |
      yq '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -s -r '.[0].metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo $object | yq -s -r '.[0].metadata.annotations."prometheus.io/port"' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo $object | yq -s -r '.[0].metadata.annotations."prometheus.io/scrape"' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

#--------------------------------------------------------------------
# replicas

@test "terminatingGateways/Deployment: replicas defaults to 2" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "terminatingGateways/Deployment: replicas can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.replicas=3' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}

@test "terminatingGateways/Deployment: replicas can be set through specific gateway, overrides default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.replicas=3' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].replicas=12' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "12" ]
}

#--------------------------------------------------------------------
# extraVolumes

@test "terminatingGateways/Deployment: adds extra volume" {
  cd `chart_dir`

  # Test that it defines it
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.extraVolumes[0].type=configMap' \
      --set 'terminatingGateways.defaults.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r -s '.[0].spec.template.spec.volumes[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.configMap.name' | tee /dev/stderr)
  [ "${actual}" = "foo" ]

  local actual=$(echo $object |
      yq -r '.configMap.secretName' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  # Test that it mounts it
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.extraVolumes[0].type=configMap' \
      --set 'terminatingGateways.defaults.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r -s '.[0].spec.template.spec.containers[0].volumeMounts[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/userconfig/foo" ]
}

@test "terminatingGateways/Deployment: adds extra secret volume" {
  cd `chart_dir`

  # Test that it defines it
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.extraVolumes[0].type=secret' \
      --set 'terminatingGateways.defaults.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r -s '.[0].spec.template.spec.volumes[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.secret.name' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo $object |
      yq -r '.secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo" ]

  # Test that it mounts it
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.extraVolumes[0].type=configMap' \
      --set 'terminatingGateways.defaults.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r -s '.[0].spec.template.spec.containers[0].volumeMounts[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/userconfig/foo" ]
}

@test "terminatingGateways/Deployment: adds extra secret volume with items" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.extraVolumes[0].type=secret' \
      --set 'terminatingGateways.defaults.extraVolumes[0].name=foo' \
      --set 'terminatingGateways.defaults.extraVolumes[0].items[0].key=key' \
      --set 'terminatingGateways.defaults.extraVolumes[0].items[0].path=path' \
      . | tee /dev/stderr |
      yq -c -s '.[0].spec.template.spec.volumes[] | select(.name == "userconfig-foo")' | tee /dev/stderr)
  [ "${actual}" = "{\"name\":\"userconfig-foo\",\"secret\":{\"secretName\":\"foo\",\"items\":[{\"key\":\"key\",\"path\":\"path\"}]}}" ]
}

@test "terminatingGateways/Deployment: adds extra secret volume through specific gateway overriding defaults" {
  cd `chart_dir`

  # Test that it defines it
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.extraVolumes[0].type=secret' \
      --set 'terminatingGateways.defaults.extraVolumes[0].name=default-foo' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].extraVolumes[0].type=secret' \
      --set 'terminatingGateways.gateways[0].extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r -s '.[0].spec.template.spec.volumes[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.secret.name' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo $object |
      yq -r '.secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo" ]

  # Test that it mounts it
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.extraVolumes[0].type=configMap' \
      --set 'terminatingGateways.defaults.extraVolumes[0].name=default-foo' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].extraVolumes[0].type=secret' \
      --set 'terminatingGateways.gateways[0].extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r -s '.[0].spec.template.spec.containers[0].volumeMounts[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/userconfig/foo" ]
}

#--------------------------------------------------------------------
# resources

@test "terminatingGateways/Deployment: resources has default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "100Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "100m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "100Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "100m" ]
}

@test "terminatingGateways/Deployment: resources can be set through defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.resources.requests.memory=memory' \
      --set 'terminatingGateways.defaults.resources.requests.cpu=cpu' \
      --set 'terminatingGateways.defaults.resources.limits.memory=memory2' \
      --set 'terminatingGateways.defaults.resources.limits.cpu=cpu2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].resources' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.requests.memory' | tee /dev/stderr)
  [ "${actual}" = "memory" ]

  local actual=$(echo $object | yq -r '.requests.cpu' | tee /dev/stderr)
  [ "${actual}" = "cpu" ]

  local actual=$(echo $object | yq -r '.limits.memory' | tee /dev/stderr)
  [ "${actual}" = "memory2" ]

  local actual=$(echo $object | yq -r '.limits.cpu' | tee /dev/stderr)
  [ "${actual}" = "cpu2" ]
}

@test "terminatingGateways/Deployment: resources can be set through specific gateway, overriding defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.resources.requests.memory=memory' \
      --set 'terminatingGateways.defaults.resources.requests.cpu=cpu' \
      --set 'terminatingGateways.defaults.resources.limits.memory=memory2' \
      --set 'terminatingGateways.defaults.resources.limits.cpu=cpu2' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].resources.requests.memory=gwmemory' \
      --set 'terminatingGateways.gateways[0].resources.requests.cpu=gwcpu' \
      --set 'terminatingGateways.gateways[0].resources.limits.memory=gwmemory2' \
      --set 'terminatingGateways.gateways[0].resources.limits.cpu=gwcpu2' \
      . | tee /dev/stderr |
      yq -s '.[0].spec.template.spec.containers[0].resources' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.requests.memory' | tee /dev/stderr)
  [ "${actual}" = "gwmemory" ]

  local actual=$(echo $object | yq -r '.requests.cpu' | tee /dev/stderr)
  [ "${actual}" = "gwcpu" ]

  local actual=$(echo $object | yq -r '.limits.memory' | tee /dev/stderr)
  [ "${actual}" = "gwmemory2" ]

  local actual=$(echo $object | yq -r '.limits.cpu' | tee /dev/stderr)
  [ "${actual}" = "gwcpu2" ]
}

#--------------------------------------------------------------------
# affinity

@test "terminatingGateways/Deployment: affinity defaults to one per node" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].topologyKey' | tee /dev/stderr)
  [ "${actual}" = "kubernetes.io/hostname" ]
}

@test "terminatingGateways/Deployment: affinity can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.affinity=key: value' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.affinity.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "terminatingGateways/Deployment: affinity can be set through specific gateway, overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.affinity=key: value' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].affinity=key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.affinity.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# tolerations

@test "terminatingGateways/Deployment: no tolerations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.tolerations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "terminatingGateways/Deployment: tolerations can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.tolerations=- key: value' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "terminatingGateways/Deployment: tolerations can be set through specific gateway, overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.tolerations=- key: value' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].tolerations=- key: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# topologySpreadConstraints

@test "terminatingGateways/Deployment: topologySpreadConstraints not set by default" {  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .topologySpreadConstraints? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "terminatingGateways/Deployment: topologySpreadConstraints can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.topologySpreadConstraints=- key: value' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.topologySpreadConstraints[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "terminatingGateways/Deployment: topologySpreadConstraints can be set through specific gateway, overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.topologySpreadConstraints=foobar' \
      --set 'terminatingGateways.defaults.topologySpreadConstraints=- key: value' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].topologySpreadConstraints=- key: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.topologySpreadConstraints[0].key' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "terminatingGateways/Deployment: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "terminatingGateways/Deployment: can set a nodeSelector through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.nodeSelector=key: value' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.nodeSelector.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "terminatingGateways/Deployment: can set a nodeSelector through specific gateway, overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.nodeSelector=key: value' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].nodeSelector=key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.nodeSelector.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "terminatingGateways/Deployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "terminatingGateways/Deployment: can set a priorityClassName through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.priorityClassName=name' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "name" ]
}

@test "terminatingGateways/Deployment: can set a priorityClassName per gateway overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.priorityClassName=name' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].priorityClassName=priority' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "priority" ]
}

#--------------------------------------------------------------------
# annotations

@test "terminatingGateways/Deployment: no extra annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "terminatingGateways/Deployment: extra annotations can be set through defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.annotations=key1: value1
key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq '. | length' | tee /dev/stderr)
  [ "${actual}" = "4" ]

  local actual=$(echo $object | yq -r '.key1' | tee /dev/stderr)
  [ "${actual}" = "value1" ]

  local actual=$(echo $object | yq -r '.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

@test "terminatingGateways/Deployment: extra annotations can be set through specific gateway" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].annotations=key1: value1
key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq '. | length' | tee /dev/stderr)
  [ "${actual}" = "4" ]

  local actual=$(echo $object | yq -r '.key1' | tee /dev/stderr)
  [ "${actual}" = "value1" ]

  local actual=$(echo $object | yq -r '.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

@test "terminatingGateways/Deployment: extra annotations can be set through defaults and specific gateway" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.annotations=defaultkey: defaultvalue' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[0].annotations=key1: value1
key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq '. | length' | tee /dev/stderr)
  [ "${actual}" = "5" ]

  local actual=$(echo $object | yq -r '.defaultkey' | tee /dev/stderr)
  [ "${actual}" = "defaultvalue" ]

  local actual=$(echo $object | yq -r '.key1' | tee /dev/stderr)
  [ "${actual}" = "value1" ]

  local actual=$(echo $object | yq -r '.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# terminating-gateway-init init container command

@test "terminatingGateways/Deployment: terminating-gateway-init init container defaults" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/terminating-gateways-deployment.yaml \
    --set 'terminatingGateways.enabled=true' \
    --set 'connectInject.enabled=true' \
    . | tee /dev/stderr |
    yq -s -r '.[0].spec.template.spec.initContainers | map(select(.name == "terminating-gateway-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-api-timeout=5s \
  -gateway-kind="terminating-gateway" \
  -consul-node-name="k8s-service-mesh" \
  -proxy-id-file=/consul/service/proxyid \
  -service-name=terminating-gateway \
  -log-level=info \
  -log-json=false'

  [ "${actual}" = "${exp}" ]
}

@test "terminatingGateways/Deployment: terminating-gateway-init init container with acls.mangageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/terminating-gateways-deployment.yaml \
    --set 'terminatingGateways.enabled=true' \
    --set 'connectInject.enabled=true' \
    --set 'global.acls.manageSystemACLs=true' \
    . | tee /dev/stderr |
    yq -s -r '.[0].spec.template.spec.initContainers | map(select(.name == "terminating-gateway-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-api-timeout=5s \
  -gateway-kind="terminating-gateway" \
  -consul-node-name="k8s-service-mesh" \
  -proxy-id-file=/consul/service/proxyid \
  -acl-auth-method=release-name-consul-k8s-component-auth-method \
  -acl-token-sink=/consul/service/acl-token \
  -service-name=terminating-gateway \
  -log-level=info \
  -log-json=false'

  [ "${actual}" = "${exp}" ]
}

#--------------------------------------------------------------------
# consul namespaces

@test "terminatingGateways/Deployment: namespace annotation is not present by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq -r 'any(contains("consul.hashicorp.com/gateway-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}


@test "terminatingGateways/Deployment: consulNamespace is set as an annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'terminatingGateways.defaults.consulNamespace=namespace' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations."consul.hashicorp.com/gateway-namespace"' | tee /dev/stderr)

  [ "${actual}" = "namespace" ]
}

@test "terminatingGateways/Deployment: consulNamespace is set as an annotation when set on the individual gateway" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'terminatingGateways.defaults.consulNamespace=namespace' \
      --set 'terminatingGateways.gateways[0].name=terminating-gateway' \
      --set 'terminatingGateways.gateways[0].consulNamespace=new-namespace' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations."consul.hashicorp.com/gateway-namespace"' | tee /dev/stderr)

  [ "${actual}" = "new-namespace" ]
}

#--------------------------------------------------------------------
# partitions

@test "terminatingGateways/Deployment: partition command flag is not present by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.command | any(contains("-partition"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

# TODO re-enable this when integrating dataplane
# @test "terminatingGateways/Deployment: partition command flag is specified through partition name" {
#   cd `chart_dir`
#   local object=$(helm template \
#       -s templates/terminating-gateways-deployment.yaml  \
#       --set 'terminatingGateways.enabled=true' \
#       --set 'connectInject.enabled=true' \
#       --set 'global.enableConsulNamespaces=true' \
#       --set 'global.adminPartitions.enabled=true' \
#       --set 'global.adminPartitions.name=default' \
#       . | tee /dev/stderr |
#       yq -s -r '.[0].spec.template.spec.containers[0]' | tee /dev/stderr)

#   local actual=$(echo $object | yq -r '.command | any(contains("-partition=default"))' | tee /dev/stderr)
#   [ "${actual}" = "true" ]
# }

@test "terminatingGateways/Deployment: fails if admin partitions are enabled but namespaces aren't" {
  cd `chart_dir`
    run helm template \
        -s templates/terminating-gateways-deployment.yaml  \
        --set 'terminatingGateways.enabled=true' \
        --set 'connectInject.enabled=true' \
        --set 'global.enableConsulNamespaces=false' \
        --set 'global.adminPartitions.enabled=true' .

    [ "$status" -eq 1 ]
    [[ "$output" =~ "global.enableConsulNamespaces must be true if global.adminPartitions.enabled=true" ]]
}

#--------------------------------------------------------------------
# multiple gateways

@test "terminatingGateways/Deployment: multiple gateways" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[1].name=gateway2' \
      . | tee /dev/stderr |
      yq -s -r '.' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-gateway1" ]

  local actual=$(echo $object | yq -r '.[1].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-gateway2" ]

  local actual=$(echo $object | yq '.[0] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object | yq '.[1] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object | yq '.[2] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# Vault

@test "terminatingGateways/Deployment: configures server CA to come from vault when vault is enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check annotations
  local actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-init-first"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "carole" ]

  local actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = "foo" ]

  local actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = $'{{- with secret \"foo\" -}}\n{{- .Data.certificate -}}\n{{- end -}}' ]
}

@test "terminatingGateways/Deployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/terminating-gateways-deployment.yaml  \
    --set 'terminatingGateways.enabled=true' \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enabled=true' \
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

@test "terminatingGateways/Deployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/terminating-gateways-deployment.yaml  \
    --set 'terminatingGateways.enabled=true' \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enabled=true' \
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

@test "terminatingGateways/Deployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/terminating-gateways-deployment.yaml  \
    --set 'terminatingGateways.enabled=true' \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enabled=true' \
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

@test "terminatingGateways/Deployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/terminating-gateways-deployment.yaml  \
    --set 'terminatingGateways.enabled=true' \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enabled=true' \
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

#--------------------------------------------------------------------
# Vault agent annotations

@test "terminatingGateways/Deployment: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations | del(."consul.hashicorp.com/connect-inject") | del(."vault.hashicorp.com/agent-inject") | del(."consul.hashicorp.com/gateway-kind") | del(."vault.hashicorp.com/role")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "terminatingGateways/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
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
