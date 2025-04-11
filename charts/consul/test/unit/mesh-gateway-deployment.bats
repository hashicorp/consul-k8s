#!/usr/bin/env bats

load _helpers

@test "meshGateway/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      .
}

@test "meshGateway/Deployment: enabled with meshGateway, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# prerequisites

@test "meshGateway/Deployment: fails if connectInject.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=false' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.enabled must be true" ]]
}

#--------------------------------------------------------------------
# annotations

@test "meshGateway/Deployment: no extra annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations | length' | tee /dev/stderr)
  [ "${actual}" = "8" ]
}

@test "meshGateway/Deployment: extra annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.annotations=key1: value1
key2: value2' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations | length' | tee /dev/stderr)
  [ "${actual}" = "10" ]
}

#--------------------------------------------------------------------
# metrics

@test "meshGateway/Deployment: when global.metrics.enabled=true, adds prometheus scrape=true annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
       yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/scrape"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: when global.metrics.enabled=true, adds prometheus port=20200 annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
       yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/port"' | tee /dev/stderr)
  [ "${actual}" = "20200" ]
}

@test "meshGateway/Deployment: when global.metrics.enabled=true, adds prometheus path=/metrics annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      . | tee /dev/stderr |
       yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/metrics" ]
}

@test "meshGateway/Deployment: when global.metrics.enabled=true, and mesh gateways annotation for prometheus path is specified, it uses the specified annotation rather than default." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'meshGateway.annotations=prometheus.io/path: /anew/path' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/anew/path" ]
}

@test "meshGateway/Deployment: when global.metrics.enableGatewayMetrics=false, does not set annotations" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: when global.metrics.enabled=false, does not set annotations" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: sets server-watch-disabled flag when externalServers.enabled and externalServers.skipServerWatch is true" {
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

  local actual=$(echo $object | yq -r '. | any(contains("-server-watch-disabled"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# replicas

@test "meshGateway/Deployment: replicas defaults to 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "meshGateway/Deployment: replicas can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.replicas=3' \
      . | tee /dev/stderr |
      yq -r '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}

#--------------------------------------------------------------------
# affinity

@test "meshGateway/Deployment: affinity defaults to null" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.affinity' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: affinity can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.affinity=key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.affinity.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# tolerations

@test "meshGateway/Deployment: no tolerations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.tolerations=- key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# topologySpreadConstraints

@test "meshGateway/Deployment: topologySpreadConstraints not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .topologySpreadConstraints? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: topologySpreadConstraints can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.topologySpreadConstraints=foobar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.topologySpreadConstraints == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# hostNetwork

@test "meshGateway/Deployment: hostNetwork is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.hostNetwork' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: hostNetwork can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.hostNetwork=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.hostNetwork' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# dnsPolicy

@test "meshGateway/Deployment: no dnsPolicy by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.dnsPolicy' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: dnsPolicy can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.dnsPolicy=ClusterFirst' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.dnsPolicy' | tee /dev/stderr)
  [ "${actual}" = "ClusterFirst" ]
}

#--------------------------------------------------------------------
# resources

@test "meshGateway/Deployment: resources has default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "100Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "100m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "100Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = null ]
}

@test "meshGateway/Deployment: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.resources.limits.cpu=4' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.limits.cpu' | tee /dev/stderr)
  [ "${actual}" = 4 ]
}

# Test support for the deprecated method of setting a YAML string.
@test "meshGateway/Deployment: resources can be overridden with string" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.resources.limits.cpu="2000m"' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.limits.cpu' | tee /dev/stderr)
  [ "${actual}" = "2000m" ]
}

#--------------------------------------------------------------------
# init container resources

@test "meshGateway/Deployment: init container has default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "50Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "50m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "50Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "50m" ]
}

@test "meshGateway/Deployment: init container resources can be set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.initServiceInitContainer.resources.requests.memory=memory' \
      --set 'meshGateway.initServiceInitContainer.resources.requests.cpu=cpu' \
      --set 'meshGateway.initServiceInitContainer.resources.limits.memory=memory2' \
      --set 'meshGateway.initServiceInitContainer.resources.limits.cpu=cpu2' \
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
# containerPort

@test "meshGateway/Deployment: containerPort defaults to 8443" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr \
      | yq  '.spec.template.spec.containers[0]' | tee /dev/stderr)

  [ $(echo "$actual" | yq -r '.ports[0].containerPort')  = "8443" ]
  [ $(echo "$actual" | yq -r '.livenessProbe.tcpSocket.port')  = "8443" ]
  [ $(echo "$actual" | yq -r '.readinessProbe.tcpSocket.port')  = "8443" ]
}

@test "meshGateway/Deployment: containerPort can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.containerPort=9443' \
      . | tee /dev/stderr \
      | yq  '.spec.template.spec.containers[0]' | tee /dev/stderr)

  [ $(echo "$actual" | yq -r '.ports[0].containerPort')  = "9443" ]
  [ $(echo "$actual" | yq -r '.livenessProbe.tcpSocket.port')  = "9443" ]
  [ $(echo "$actual" | yq -r '.readinessProbe.tcpSocket.port')  = "9443" ]
}

#--------------------------------------------------------------------
# consulServiceName

@test "meshGateway/Deployment: fails if consulServiceName is set and acls.manageSystemACLs is true" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.consulServiceName=override' \
      --set 'global.acls.manageSystemACLs=true' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "if global.acls.manageSystemACLs is true, meshGateway.consulServiceName cannot be set" ]]
}

@test "meshGateway/Deployment: add consul-dataplane envvars on mesh-gateway container" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "DP_CREDENTIAL_LOGIN_META1") | .value' | tee /dev/stderr)
  [ "${actual}" = 'pod=$(NAMESPACE)/$(POD_NAME)' ]

  local actual=$(echo $env | jq -r '. | select(.name == "DP_CREDENTIAL_LOGIN_META2") | .value' | tee /dev/stderr)
  [ "${actual}" = "component=mesh-gateway" ]
}

#--------------------------------------------------------------------
# manageSystemACLs

@test "meshGateway/Deployment: ACL specific flags are not set when acls are disabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

      local actual=$(echo $command | yq -r '. | any(contains("credential-type=login"))'| tee /dev/stderr)
      [ "${actual}" = "false" ]

      local actual=$(echo $command | yq -r '. | any(contains("-login-bearer-path"))'| tee /dev/stderr)
      [ "${actual}" = "false" ]

      local actual=$(echo $command | yq -r '. | any(contains("-login-method"))'| tee /dev/stderr)
      [ "${actual}" = "false" ]
}

@test "meshGateway/Deployment: ACL specific flags are set when acls are enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

      local actual=$(echo $command | yq -r '. | any(contains("credential-type=login"))'| tee /dev/stderr)
      [ "${actual}" = "true" ]

      local actual=$(echo $command | yq -r '. | any(contains("-login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token"))'| tee /dev/stderr)
      [ "${actual}" = "true" ]

      local actual=$(echo $command | yq -r '. | any(contains("-login-auth-method=release-name-consul-k8s-component-auth-method"))'| tee /dev/stderr)
      [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: correct login-method and login-datacenter are set with federation is enabled and in secondary DC" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc2' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

      local actual=$(echo $command | yq -r '. | any(contains("-login-auth-method=release-name-consul-k8s-component-auth-method-dc1"))'| tee /dev/stderr)
      [ "${actual}" = "true" ]

      local actual=$(echo $command | yq -r '. | any(contains("-login-datacenter=dc2"))'| tee /dev/stderr)
      [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: correct login-partition is set with partitions is enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=other-partition' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

      local actual=$(echo $command | yq -r '. | any(contains("-login-partition=other-partition"))'| tee /dev/stderr)
      [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: init container has correct environment with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "mesh-gateway-init" ]

  local actual=$(echo $object |
      yq '[.env[8].name] | any(contains("CONSUL_LOGIN_AUTH_METHOD"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[8].value] | any(contains("release-name-consul-k8s-component-auth-method"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[9].name] | any(contains("CONSUL_LOGIN_DATACENTER"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[9].value] | any(contains("dc1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[10].name] | any(contains("CONSUL_LOGIN_META"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[10].value] | any(contains("component=mesh-gateway,pod=$(NAMESPACE)/$(POD_NAME)"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: init container has correct environment variables when tls enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "mesh-gateway-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq '[.env[8].name] | any(contains("CONSUL_USE_TLS"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[8].value] | any(contains("true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[9].name] | any(contains("CONSUL_CACERT_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[9].value] | any(contains("/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: init container has correct envs with Partitions enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=default' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "mesh-gateway-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq '[.env[8].name] | any(contains("CONSUL_PARTITION"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[8].value] | any(contains("default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[9].name] | any(contains("CONSUL_LOGIN_PARTITION"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[9].value] | any(contains("default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct env when federation enabled in non-primary datacenter" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.datacenter=dc2' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "mesh-gateway-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq '[.env[10].name] | any(contains("CONSUL_LOGIN_AUTH_METHOD"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[10].value] | any(contains("release-name-consul-k8s-component-auth-method-dc2"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[11].name] | any(contains("CONSUL_LOGIN_DATACENTER"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[11].value] | any(contains("dc1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# healthchecks

@test "meshGateway/Deployment: healthchecks are on by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr \
      | yq '.spec.template.spec.containers[0]' | tee /dev/stderr )

  local liveness=$(echo "${actual}" | yq -r '.livenessProbe | length > 0' | tee /dev/stderr)
  [ "${liveness}" = "true" ]
  local readiness=$(echo "${actual}" | yq -r '.readinessProbe | length > 0' | tee /dev/stderr)
  [ "${readiness}" = "true" ]
}

#--------------------------------------------------------------------
# hostPort

@test "meshGateway/Deployment: no hostPort by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].ports[0].hostPort' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: can set a hostPort" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.hostPort=443' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].ports[0].hostPort' | tee /dev/stderr)

  [ "${actual}" = "443" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "meshGateway/Deployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: can set a priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.priorityClassName=name' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "name" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "meshGateway/Deployment: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: can set a nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.nodeSelector=key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector.key' | tee /dev/stderr)

  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "meshGateway/Deployment: sets TLS args when global.tls.disabled" {
  cd `chart_dir`
  local flags=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $flags | yq -r '. | any(contains("-tls-disabled"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

@test "meshGateway/Deployment: sets TLS args when global.tls.enabled" {
  cd `chart_dir`
  local flags=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $flags | yq -r '. | any(contains("-ca-certs=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

@test "meshGateway/Deployment: sets external server args when global.tls.enabled and externalServers.enabled" {
  cd `chart_dir`
  local flags=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.useSystemRoots=true' \
      --set 'externalServers.tlsServerName=foo.tls.server' \
      --set 'externalServers.hosts[0]=host' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $flags | yq -r '. | any(contains("-ca-certs=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = 'false' ]

  local actual=$(echo $flags | yq -r '. | any(contains("-tls-server-name=foo.tls.server"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

@test "meshGateway/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: CA cert volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "meshGateway/Deployment: CA cert volume mount present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "meshGateway/Deployment: CA cert volume is not present when TLS is enabled with externalServers and useSystemRoots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "meshGateway/Deployment: CA cert volume mount is not present when TLS is enabled with externalServers and useSystemRoots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

##--------------------------------------------------------------------
## mesh-gateway service annotations

@test "meshGateway/Deployment: mesh-gateway annotations containerPort and wanAddress.port can be changed" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.containerPort=8888' \
      --set 'meshGateway.wanAddress.source=NodeIP' \
      --set 'meshGateway.wanAddress.port=9999' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations ' | tee /dev/stderr)

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/mesh-gateway-container-port"]' | tee /dev/stderr)
    [ "${actual}" = "8888" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]' | tee /dev/stderr)
    [ "${actual}" = "NodeIP" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-port"]' | tee /dev/stderr)
    [ "${actual}" = "9999" ]
}

@test "meshGateway/Deployment: mesh-gateway annotations wanAddress.source=NodeIP" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=NodeIP' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations ' | tee /dev/stderr)

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/mesh-gateway-container-port"]' | tee /dev/stderr)
    [ "${actual}" = "8443" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]' | tee /dev/stderr)
    [ "${actual}" = "NodeIP" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-port"]' | tee /dev/stderr)
    [ "${actual}" = "443" ]
}

@test "meshGateway/Deployment: mesh-gateway annotations wanAddress.source=NodeName" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=NodeName' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations ' | tee /dev/stderr)

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/mesh-gateway-container-port"]' | tee /dev/stderr)
    [ "${actual}" = "8443" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]' | tee /dev/stderr)
    [ "${actual}" = "NodeName" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-port"]' | tee /dev/stderr)
    [ "${actual}" = "443" ]
}

@test "meshGateway/Deployment: mesh-gateway-init init container wanAddress.source=Static fails if wanAddress.static is empty" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Static' \
      --set 'meshGateway.wanAddress.static=' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "if meshGateway.wanAddress.source=Static then meshGateway.wanAddress.static cannot be empty" ]]
}

@test "meshGateway/Deployment: mesh-gateway-init init container wanAddress.source=Static" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Static' \
      --set 'meshGateway.wanAddress.static=example.com' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations ' | tee /dev/stderr)

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/mesh-gateway-container-port"]' | tee /dev/stderr)
    [ "${actual}" = "8443" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]' | tee /dev/stderr)
    [ "${actual}" = "Static" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-address-static"]' | tee /dev/stderr)
    [ "${actual}" = "example.com" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-port"]' | tee /dev/stderr)
    [ "${actual}" = "443" ]
}

@test "meshGateway/Deployment: mesh-gateway-init init container wanAddress.source=Service, type=LoadBalancer" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.wanAddress.port=ignored' \
      --set 'meshGateway.service.type=LoadBalancer' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations ' | tee /dev/stderr)

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/mesh-gateway-container-port"]' | tee /dev/stderr)
    [ "${actual}" = "8443" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]' | tee /dev/stderr)
    [ "${actual}" = "Service" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-port"]' | tee /dev/stderr)
    [ "${actual}" = "443" ]
}

@test "meshGateway/Deployment: mesh-gateway-init init container wanAddress.source=Service, type=NodePort" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.service.nodePort=9999' \
      --set 'meshGateway.service.type=NodePort' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations ' | tee /dev/stderr)

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/mesh-gateway-container-port"]' | tee /dev/stderr)
    [ "${actual}" = "8443" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]' | tee /dev/stderr)
    [ "${actual}" = "Service" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-port"]' | tee /dev/stderr)
    [ "${actual}" = "9999" ]
}

@test "meshGateway/Deployment: mesh-gateway-init init container wanAddress.source=Service, type=NodePort fails if service.nodePort is null" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.service.type=NodePort' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "if meshGateway.wanAddress.source=Service and meshGateway.service.type=NodePort, meshGateway.service.nodePort must be set" ]]
}

@test "meshGateway/Deployment: mesh-gateway-init init container wanAddress.source=Service, type=ClusterIP" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.wanAddress.port=ignored' \
      --set 'meshGateway.service.type=ClusterIP' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations ' | tee /dev/stderr)

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/mesh-gateway-container-port"]' | tee /dev/stderr)
    [ "${actual}" = "8443" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]' | tee /dev/stderr)
    [ "${actual}" = "Service" ]

    local actual=$(echo $annotations | yq -r '.["consul.hashicorp.com/gateway-wan-port"]' | tee /dev/stderr)
    [ "${actual}" = "443" ]
}

@test "meshGateway/Deployment: CA cert volume mount present on the init container when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "meshGateway/Deployment: CA cert volume mount present is not present on the init container when TLS is enabled with externalServers and useSystemRoots" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# meshGateway.globalMode [DEPRECATED]

@test "meshGateway/Deployment: fails if meshGateway.globalMode is set" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.globalMode=something' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "meshGateway.globalMode is no longer supported; instead, you must migrate to CRDs (see www.consul.io/docs/k8s/crds/upgrade-to-crds)" ]]
}

#--------------------------------------------------------------------
# partitions

@test "meshGateway/Deployment: partitions options disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("partition"))' | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "meshGateway/Deployment: partition name set on initContainer with .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0].env[8].value | contains("default")' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: partition name set on container with .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("partition=default"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: fails if namespaces are disabled and .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=false' \
      --set 'meshGateway.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.enableConsulNamespaces must be true if global.adminPartitions.enabled=true" ]]
}

#--------------------------------------------------------------------
# Vault

@test "meshGateway/Deployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/mesh-gateway-deployment.yaml  \
    --set 'connectInject.enabled=true' \
    --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/mesh-gateway-deployment.yaml  \
    --set 'connectInject.enabled=true' \
    --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/mesh-gateway-deployment.yaml  \
    --set 'connectInject.enabled=true' \
    --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/mesh-gateway-deployment.yaml  \
    --set 'connectInject.enabled=true' \
    --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: vault tls annotations are set when tls is enabled" {
  cd `chart_dir`
  local obj=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual="$(echo $obj |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/cert/ca\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $obj |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki_int/cert/ca" ]

  local actual="$(echo $obj |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-init-first"]' | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$(echo $obj |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$(echo $obj |
      yq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)"
  [ "${actual}" = "test" ]

  actual=$(echo $obj | jq -r '.spec.initContainers[0].env[] | select(.name == "CONSUL_CACERT_FILE").value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/serverca.crt" ]

  actual=$(echo $obj | jq -r '.spec.containers[0].args | any(contains("-ca-certs=/vault/secrets/serverca.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set without vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

#--------------------------------------------------------------------
# Vault agent annotations

@test "meshGateway/Deployment: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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
      del(."consul.hashicorp.com/gateway-kind") |
      del(."consul.hashicorp.com/gateway-wan-address-source") |
      del(."consul.hashicorp.com/mesh-gateway-container-port") |
      del(."consul.hashicorp.com/gateway-wan-address-static") |
      del(."consul.hashicorp.com/gateway-wan-port") |
      del(."consul.hashicorp.com/gateway-consul-service-name")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "meshGateway/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

#--------------------------------------------------------------------
# global.cloud

@test "meshGateway/Deployment: fails when global.cloud.enabled is true and global.cloud.clientId.secretName is not set but global.cloud.clientSecret.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: sets TLS server name if global.cloud.enabled is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component") | del(."consul.hashicorp.com/connect-inject-managed-by")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "meshGateway/Deployment: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "meshGateway/Deployment: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml \
      --set 'meshGateway.enabled=true' \
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

@test "meshGateway/Deployment: use global.logLevel by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: override global.logLevel when meshGateway.logLevel is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'meshGateway.logLevel=warn' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=warn"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: use global.logLevel by default for dataplane container" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: override global.logLevel when meshGateway.logLevel is set for dataplane container" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'meshGateway.logLevel=warn' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=warn"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# security context

@test "meshGateway/Deployment: don't drop ALL capabilities when hostNetwork=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.hostNetwork=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].securityContext' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.capabilities.drop | length') -eq 0 ]
}

@test "meshGateway/Deployment: drop ALL capabilities when hostNetwork!=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].securityContext' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.capabilities.drop[0]') = "ALL" ]
}