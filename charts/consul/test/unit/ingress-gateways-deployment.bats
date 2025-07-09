#!/usr/bin/env bats

load _helpers

@test "ingressGateways/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      .
}

@test "ingressGateways/Deployment: enabled with ingressGateways, connectInject enabled, has default gateway name" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s '.[0]' | tee /dev/stderr)

  local actual=$(echo $object | yq '. | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object | yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-ingress-gateway" ]
}

@test "ingressGateways/Deployment: serviceAccountName is set properly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'ingress.defaults.consulNamespace=namespace' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.serviceAccountName' | tee /dev/stderr)

  [ "${actual}" = "release-name-consul-ingress-gateway" ]
}

#--------------------------------------------------------------------
# prerequisites

@test "ingressGateways/Deployment: fails if connectInject.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=false' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.enabled must be true" ]]
}

@test "ingressGateways/Deployment: fails if there are duplicate gateway names" {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.gateways[0].name=foo' \
      --set 'ingressGateways.gateways[1].name=foo' \
      --set 'connectInject.enabled=true' \
      --set 'global.enabled=true' \
      --set 'client.enabled=true' .
  echo "status: $output"
  [ "$status" -eq 1 ]
  [[ "$output" =~ "ingress gateways must have unique names but found duplicate name foo" ]]
}

@test "ingressGateways/Deployment: fails if a terminating gateway has the same name as an ingress gateway" {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'terminatingGateways.gateways[0].name=foo' \
      --set 'ingressGateways.gateways[0].name=foo' \
      --set 'connectInject.enabled=true' \
      --set 'global.enabled=true' \
      --set 'client.enabled=true' .
  echo "status: $output"
  [ "$status" -eq 1 ]
  [[ "$output" =~ "terminating gateways cannot have duplicate names of any ingress gateways" ]]
}
#--------------------------------------------------------------------
# dataplaneImage

@test "ingressGateways/Deployment: dataplane image can be set using the global value" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.imageConsulDataplane=new/image' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "new/image" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "ingressGateways/Deployment: sets flags when global.tls.enabled is false" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '. | any(contains("-tls-disabled"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: sets flags when global.tls.enabled is false and global.enableConsulNamespaces=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=false' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '. | any(contains("-tls-disabled"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: sets TLS flags when global.tls.enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '. | any(contains("-ca-certs=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
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

@test "ingressGateways/Deployment: CA cert volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "ingressGateways/Deployment: Adds consul envvars on ingress-gateway-init init container when ACLs are enabled and tls is enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_LOGIN_AUTH_METHOD") | .value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-k8s-component-auth-method" ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_LOGIN_DATACENTER") | .value' | tee /dev/stderr)
  [ "${actual}" = "dc1" ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_LOGIN_META") | .value' | tee /dev/stderr)
  [ "${actual}" = 'component=ingress-gateway,pod=$(NAMESPACE)/$(POD_NAME)' ]
}

@test "ingressGateways/Deployment: ACL flags are not set when acls are disabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'global.acls.manageSystemACLs=false' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '. | any(contains("-login-bearer-path"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object | yq -r '. | any(contains("-login-method"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object | yq -r '. | any(contains("-credential-type=login"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ingressGateways/Deployment: command flags are set when acls are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '. | any(contains("-login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object | yq -r '. | any(contains("-login-auth-method=release-name-consul-k8s-component-auth-method"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object | yq -r '. | any(contains("-credential-type=login"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: add consul-dataplane envvars on ingress-gateway container" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "DP_ENVOY_READY_BIND_ADDRESS") | .valueFrom.fieldRef.fieldPath' | tee /dev/stderr)
  [ "${actual}" = "status.podIP" ]

  local actual=$(echo $env | jq -r '. | select(.name == "DP_CREDENTIAL_LOGIN_META1") | .value' | tee /dev/stderr)
  [ "${actual}" = 'pod=$(NAMESPACE)/$(POD_NAME)' ]

  local actual=$(echo $env | jq -r '. | select(.name == "DP_CREDENTIAL_LOGIN_META2") | .value' | tee /dev/stderr)
  [ "${actual}" = "component=ingress-gateway" ]
}

#--------------------------------------------------------------------
# metrics

@test "ingressGateways/Deployment: when global.metrics.enabled=true, adds prometheus scrape=true annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/scrape"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: when global.metrics.enabled=true, adds prometheus port=20200 annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/port"' | tee /dev/stderr)
  [ "${actual}" = "20200" ]
}

@test "ingressGateways/Deployment: when global.metrics.enabled=true, adds prometheus path=/metrics annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/metrics" ]
}

@test "ingressGateways/Deployment: when global.metrics.enabled=true, and ingress gateways annotation for prometheus path is specified, it uses the specified annotation rather than default." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'ingressGateways.defaults.annotations=prometheus.io/path: /anew/path' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/anew/path" ]
}

@test "ingressGateways/Deployment: when global.metrics.enableGatewayMetrics=false, does not set proxy setting" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
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

@test "ingressGateways/Deployment: when global.metrics.enabled=false, does not set proxy setting" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
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
# externalServers.skipServerWatch

@test "ingressGateways/Deployment: sets server-watch-disabled flag when externalServers.enabled and externalServers.skipServerWatch is true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=false' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      --set 'externalServers.skipServerWatch=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '. | any(contains("-server-watch-disabled=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# replicas

@test "ingressGateways/Deployment: replicas defaults to 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "ingressGateways/Deployment: replicas can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.replicas=3' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}

@test "ingressGateways/Deployment: replicas can be set through specific gateway, overrides default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.replicas=3' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].replicas=12' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "12" ]
}

#--------------------------------------------------------------------
# ports

@test "ingressGateways/Deployment: has default ports" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].ports' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].containerPort' | tee /dev/stderr)
  [ "${actual}" = "21000" ]

  local actual=$(echo $object | yq -r '.[0].name' | tee /dev/stderr)
  [ "${actual}" = "gateway-health" ]

  local actual=$(echo $object | yq -r '.[1].containerPort' | tee /dev/stderr)
  [ "${actual}" = "8080" ]

  local actual=$(echo $object | yq -r '.[1].name' | tee /dev/stderr)
  [ "${actual}" = "gateway-0" ]

  local actual=$(echo $object | yq -r '.[2].containerPort' | tee /dev/stderr)
  [ "${actual}" = "8443" ]

  local actual=$(echo $object | yq -r '.[2].name' | tee /dev/stderr)
  [ "${actual}" = "gateway-1" ]
}

@test "ingressGateways/Deployment: can set ports through defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.service.ports[0].port=1234' \
      --set 'ingressGateways.defaults.service.ports[1].port=4444' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].ports' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].containerPort' | tee /dev/stderr)
  [ "${actual}" = "21000" ]

  local actual=$(echo $object | yq -r '.[0].name' | tee /dev/stderr)
  [ "${actual}" = "gateway-health" ]

  local actual=$(echo $object | yq -r '.[1].containerPort' | tee /dev/stderr)
  [ "${actual}" = "1234" ]

  local actual=$(echo $object | yq -r '.[1].name' | tee /dev/stderr)
  [ "${actual}" = "gateway-0" ]

  local actual=$(echo $object | yq -r '.[2].containerPort' | tee /dev/stderr)
  [ "${actual}" = "4444" ]

  local actual=$(echo $object | yq -r '.[2].name' | tee /dev/stderr)
  [ "${actual}" = "gateway-1" ]
}

@test "ingressGateways/Deployment: can set ports through specific gateway overriding defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].service.ports[0].port=1234' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].ports' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].containerPort' | tee /dev/stderr)
  [ "${actual}" = "21000" ]

  local actual=$(echo $object | yq -r '.[0].name' | tee /dev/stderr)
  [ "${actual}" = "gateway-health" ]

  local actual=$(echo $object | yq -r '.[1].containerPort' | tee /dev/stderr)
  [ "${actual}" = "1234" ]

  local actual=$(echo $object | yq -r '.[1].name' | tee /dev/stderr)
  [ "${actual}" = "gateway-0" ]
}

#--------------------------------------------------------------------
# resources

@test "ingressGateways/Deployment: resources has default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "100Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "100m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "100Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = null ]
}

@test "ingressGateways/Deployment: resources can be set through defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.resources.requests.memory=memory' \
      --set 'ingressGateways.defaults.resources.requests.cpu=cpu' \
      --set 'ingressGateways.defaults.resources.limits.memory=memory2' \
      --set 'ingressGateways.defaults.resources.limits.cpu=cpu2' \
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

@test "ingressGateways/Deployment: resources can be set through specific gateway, overriding defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.resources.requests.memory=memory' \
      --set 'ingressGateways.defaults.resources.requests.cpu=cpu' \
      --set 'ingressGateways.defaults.resources.limits.memory=memory2' \
      --set 'ingressGateways.defaults.resources.limits.cpu=cpu2' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].resources.requests.memory=gwmemory' \
      --set 'ingressGateways.gateways[0].resources.requests.cpu=gwcpu' \
      --set 'ingressGateways.gateways[0].resources.limits.memory=gwmemory2' \
      --set 'ingressGateways.gateways[0].resources.limits.cpu=gwcpu2' \
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
# init container resources

@test "ingressGateways/Deployment: init container has default resources" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.initContainers[0].resources' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.requests.memory' | tee /dev/stderr)
  [ "${actual}" = "50Mi" ]

  local actual=$(echo $object | yq -r '.requests.cpu' | tee /dev/stderr)
  [ "${actual}" = "50m" ]

  local actual=$(echo $object | yq -r '.limits.memory' | tee /dev/stderr)
  [ "${actual}" = "50Mi" ]

  local actual=$(echo $object | yq -r '.limits.cpu' | tee /dev/stderr)
  [ "${actual}" = "50m" ]
}

#--------------------------------------------------------------------
# affinity

@test "ingressGateways/Deployment: affinity defaults to null" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.affinity' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ingressGateways/Deployment: affinity can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.affinity=key: value' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.affinity.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "ingressGateways/Deployment: affinity can be set through specific gateway, overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.affinity=key: value' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].affinity=key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.affinity.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# tolerations

@test "ingressGateways/Deployment: no tolerations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.tolerations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ingressGateways/Deployment: tolerations can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.tolerations=- key: value' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "ingressGateways/Deployment: tolerations can be set through specific gateway, overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.tolerations=- key: value' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].tolerations=- key: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# topologySpreadConstraints

@test "ingressGateways/Deployment: topologySpreadConstraints not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .topologySpreadConstraints? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: topologySpreadConstraints can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.topologySpreadConstraints=- key: value' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.topologySpreadConstraints[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "ingressGateways/Deployment: topologySpreadConstraints can be set through specific gateway, overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.topologySpreadConstraints=foobar' \
      --set 'ingressGateways.defaults.topologySpreadConstraints=- key: value' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].topologySpreadConstraints=- key: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.topologySpreadConstraints[0].key' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "ingressGateways/Deployment: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ingressGateways/Deployment: can set a nodeSelector through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.nodeSelector=key: value' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.nodeSelector.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "ingressGateways/Deployment: can set a nodeSelector through specific gateway, overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.nodeSelector=key: value' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].nodeSelector=key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.nodeSelector.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "ingressGateways/Deployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ingressGateways/Deployment: can set a priorityClassName through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.priorityClassName=name' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "name" ]
}

@test "ingressGateways/Deployment: can set a priorityClassName per gateway overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.priorityClassName=name' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].priorityClassName=priority' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "priority" ]
}

#--------------------------------------------------------------------
# annotations

@test "ingressGateways/Deployment: no extra annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations | length' | tee /dev/stderr)
  [ "${actual}" = "6" ]
}

@test "ingressGateways/Deployment: extra annotations can be set through defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.annotations=key1: value1
key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq '. | length' | tee /dev/stderr)
  [ "${actual}" = "8" ]

  local actual=$(echo $object | yq -r '.key1' | tee /dev/stderr)
  [ "${actual}" = "value1" ]

  local actual=$(echo $object | yq -r '.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

@test "ingressGateways/Deployment: extra annotations can be set through specific gateway" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].annotations=key1: value1
key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq '. | length' | tee /dev/stderr)
  [ "${actual}" = "8" ]

  local actual=$(echo $object | yq -r '.key1' | tee /dev/stderr)
  [ "${actual}" = "value1" ]

  local actual=$(echo $object | yq -r '.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

@test "ingressGateways/Deployment: extra annotations can be set through defaults and specific gateway" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.annotations=defaultkey: defaultvalue' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].annotations=key1: value1
key2: value2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq '. | length' | tee /dev/stderr)
  [ "${actual}" = "9" ]

  local actual=$(echo $object | yq -r '.defaultkey' | tee /dev/stderr)
  [ "${actual}" = "defaultvalue" ]

  local actual=$(echo $object | yq -r '.key1' | tee /dev/stderr)
  [ "${actual}" = "value1" ]

  local actual=$(echo $object | yq -r '.key2' | tee /dev/stderr)
  [ "${actual}" = "value2" ]
}

#--------------------------------------------------------------------
# namespaces

@test "ingressGateways/Deployment: namespace command flag is not present by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '."consul.hashicorp.com/gateway-namespace"' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ingressGateways/Deployment: namespace command flag is specified through defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'ingressGateways.defaults.consulNamespace=namespace' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '."consul.hashicorp.com/gateway-namespace"' | tee /dev/stderr)
  [ "${actual}" = "namespace" ]
}

@test "ingressGateways/Deployment: namespace annotation is specified through specific gateway overriding defaults" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'ingressGateways.defaults.consulNamespace=namespace' \
      --set 'ingressGateways.gateways[0].name=ingress-gateway' \
      --set 'ingressGateways.gateways[0].consulNamespace=new-namespace' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '."consul.hashicorp.com/gateway-namespace"' | tee /dev/stderr)
  [ "${actual}" = "new-namespace" ]
}

#--------------------------------------------------------------------
# partitions

@test "ingressGateways/Deployment: partition command flag is not present by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '. | any(contains("-partition"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ingressGateways/Deployment: partition command flag is specified through partition name" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=default' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '. | any(contains("-service-partition=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: fails if admin partitions are enabled but namespaces aren't" {
  cd `chart_dir`
    run helm template \
        -s templates/ingress-gateways-deployment.yaml  \
        --set 'ingressGateways.enabled=true' \
        --set 'connectInject.enabled=true' \
        --set 'global.enableConsulNamespaces=false' \
        --set 'global.adminPartitions.enabled=true' .

    [ "$status" -eq 1 ]
    [[ "$output" =~ "global.enableConsulNamespaces must be true if global.adminPartitions.enabled=true" ]]
}

#--------------------------------------------------------------------
# multiple gateways

@test "ingressGateways/Deployment: multiple gateways" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-role.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[1].name=gateway2' \
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

@test "ingressGateway/Deployment: vault tls annotations are set when tls is enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
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

  actual=$(echo $object | jq -r '.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  actual=$(echo $object | jq -r '.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  actual=$(echo $object | jq -r '.spec.initContainers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  actual=$(echo $object | jq -r '.spec.initContainers[0].env[] | select(.name == "CONSUL_CACERT_FILE").value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/serverca.crt" ]

  actual=$(echo $object | jq -r '.spec.containers[0].args | any(contains("-ca-certs=/vault/secrets/serverca.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateway/Deployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/ingress-gateways-deployment.yaml  \
    --set 'ingressGateways.enabled=true' \
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


@test "ingressGateway/Deployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/ingress-gateways-deployment.yaml  \
    --set 'ingressGateways.enabled=true' \
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

@test "ingressGateway/Deployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/ingress-gateways-deployment.yaml  \
    --set 'ingressGateways.enabled=true' \
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

@test "ingressGateway/Deployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/ingress-gateways-deployment.yaml  \
    --set 'ingressGateways.enabled=true' \
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

@test "ingressGateway/Deployment: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."vault.hashicorp.com/agent-inject") |
      del(."vault.hashicorp.com/role") |
      del(."consul.hashicorp.com/gateway-wan-address-source") |
      del(."consul.hashicorp.com/gateway-wan-port") |
      del(."vconsul.hashicorp.com/gateway-wan-address-source") |
      del(."consul.hashicorp.com/gateway-consul-service-name") |
      del(."consul.hashicorp.com/gateway-kind")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "ingressGateway/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
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

@test "ingressGateway/Deployment: vault namespace annotations can be set when secretsBackend.vault.vaultNamespace is set and .global.secretsBackend.vault.agentAnnotations is not set." {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
       . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/namespace"')
  [ "${actual}" = "vns" ]
}

#--------------------------------------------------------------------
# terminationGracePeriodSeconds

@test "ingressGateways/Deployment: terminationGracePeriodSeconds defaults to 10" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.terminationGracePeriodSeconds' | tee /dev/stderr)
  [ "${actual}" = "10" ]
}

@test "ingressGateways/Deployment: terminationGracePeriodSeconds can be set through defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.terminationGracePeriodSeconds' | tee /dev/stderr)
  [ "${actual}" = "5" ]
}

@test "ingressGateways/Deployment: can set terminationGracePeriodSeconds through specific gateway overriding defaults" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[0].terminationGracePeriodSeconds=30' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.terminationGracePeriodSeconds' | tee /dev/stderr)
  [ "${actual}" = "30" ]
}

#--------------------------------------------------------------------
# global.cloud

@test "ingressGateways/Deployment: fails when global.cloud.enabled is true and global.cloud.clientId.secretName is not set but global.cloud.clientSecret.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientSecret.secretName=client-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=client-resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=client-resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "ingressGateways/Deployment: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "ingressGateways/Deployment: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "ingressGateways/Deployment: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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

@test "ingressGateways/Deployment: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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

@test "ingressGateways/Deployment: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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

@test "ingressGateways/Deployment: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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

@test "ingressGateways/Deployment: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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

@test "ingressGateways/Deployment: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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

@test "ingressGateways/Deployment: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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

@test "ingressGateways/Deployment: sets TLS server name if global.cloud.enabled is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.terminationGracePeriodSeconds=5' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("-tls-server-name=server.dc1.consul"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "ingressGateways/Deployment: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component") | del(."heritage") | del(."ingress-gateway-name") | del(."consul.hashicorp.com/connect-inject-managed-by")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "ingressGateways/Deployment: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "ingressGateways/Deployment: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-deployment.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
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

@test "ingressGateways/Deployment: use global.logLevel by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0].command' | tee /dev/stderr)
  
  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: override global.logLevel when ingressGateways.logLevel is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.logLevel=warn' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0].command' | tee /dev/stderr)
  
  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=warn"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: use global.logLevel by default for dataplane container" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)
  
  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Deployment: override global.logLevel when ingressGateways.logLevel is set for dataplane container" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/ingress-gateways-deployment.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.logLevel=trace' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)
  
  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=trace"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}