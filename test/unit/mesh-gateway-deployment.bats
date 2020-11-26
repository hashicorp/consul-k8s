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

@test "meshGateway/Deployment: fails if client.grpc=false" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'client.grpc=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "client.grpc must be true" ]]
}

@test "meshGateway/Deployment: fails if global.enabled is false and clients are not explicitly enabled" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enabled=false' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled" ]]
}

@test "meshGateway/Deployment: fails if global.enabled is true but clients are explicitly disabled" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enabled=true' \
      --set 'client.enabled=false' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled" ]]
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
  [ "${actual}" = "1" ]
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
  [ "${actual}" = "3" ]
}

#--------------------------------------------------------------------
# replicas

@test "meshGateway/Deployment: replicas defaults to 2" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "2" ]
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

@test "meshGateway/Deployment: affinity defaults to one per node" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].topologyKey' | tee /dev/stderr)
  [ "${actual}" = "kubernetes.io/hostname" ]
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
# envoyImage

@test "meshGateway/Deployment: envoy image has default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "envoyproxy/envoy-alpine:v1.16.0" ]
}

@test "meshGateway/Deployment: setting meshGateway.imageEnvoy fails" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.imageEnvoy=new/image' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "meshGateway.imageEnvoy must be specified in global" ]]
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
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "100m" ]
}

@test "meshGateway/Deployment: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.resources.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

# Test support for the deprecated method of setting a YAML string.
@test "meshGateway/Deployment: resources can be overridden with string" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.resources=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
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

  [ $(echo "${actual}" | yq -r '.requests.memory') = "25Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "50m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "150Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "50m" ]
}

@test "meshGateway/Deployment: init container resources can be set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.initCopyConsulContainer.resources.requests.memory=memory' \
      --set 'meshGateway.initCopyConsulContainer.resources.requests.cpu=cpu' \
      --set 'meshGateway.initCopyConsulContainer.resources.limits.memory=memory2' \
      --set 'meshGateway.initCopyConsulContainer.resources.limits.cpu=cpu2' \
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
# lifecycle sidecar resources

@test "meshGateway/Deployment: lifecycle sidecar has default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "25Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "20m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "50Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "20m" ]
}

@test "meshGateway/Deployment: lifecycle sidecar resources can be set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-deployment.yaml \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.lifecycleSidecarContainer.resources.requests.memory=memory' \
      --set 'global.lifecycleSidecarContainer.resources.requests.cpu=cpu' \
      --set 'global.lifecycleSidecarContainer.resources.limits.memory=memory2' \
      --set 'global.lifecycleSidecarContainer.resources.limits.cpu=cpu2' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.spec.containers[1].resources' | tee /dev/stderr)

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

@test "meshGateway/Deployment: does not fail if consulServiceName is set to mesh-gateway and acls.manageSystemACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.consulServiceName=mesh-gateway' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr \
      | yq '.spec.template.spec.containers[0]' | tee /dev/stderr )

  [[ $(echo "${actual}" | yq -r '.lifecycle.preStop.exec.command' ) =~ '-id=\"mesh-gateway\"' ]]
}

@test "meshGateway/Deployment: consulServiceName can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.consulServiceName=overridden' \
      . | tee /dev/stderr \
      | yq '.spec.template.spec.containers[0]' | tee /dev/stderr )

  [[ $(echo "${actual}" | yq -r '.lifecycle.preStop.exec.command' ) =~ '-id=\"overridden\"' ]]
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

@test "meshGateway/Deployment: sets TLS env variables when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'https://$(HOST_IP):8501' ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_GRPC_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'https://$(HOST_IP):8502' ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "meshGateway/Deployment: sets TLS env variables in lifecycle sidecar when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'https://$(HOST_IP):8501' ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
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

#--------------------------------------------------------------------
# global.tls.enableAutoEncrypt

@test "meshGateway/Deployment: consul-auto-encrypt-ca-cert volume is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: consul-auto-encrypt-ca-cert volumeMount is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: get-auto-encrypt-client-ca init container is created when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
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

##--------------------------------------------------------------------
## service-init init container

@test "meshGateway/Deployment: service-init init container" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='consul-k8s service-address \
  -k8s-namespace=default \
  -name=release-name-consul-mesh-gateway \
  -output-file=/tmp/address.txt
WAN_ADDR="$(cat /tmp/address.txt)"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container with acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='consul-k8s acl-init \
  -secret-name="release-name-consul-mesh-gateway-acl-token" \
  -k8s-namespace=default \
  -token-sink-file=/consul/service/acl-token

consul-k8s service-address \
  -k8s-namespace=default \
  -name=release-name-consul-mesh-gateway \
  -output-file=/tmp/address.txt
WAN_ADDR="$(cat /tmp/address.txt)"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  -token-file=/consul/service/acl-token \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container with global.federation.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='consul-k8s service-address \
  -k8s-namespace=default \
  -name=release-name-consul-mesh-gateway \
  -output-file=/tmp/address.txt
WAN_ADDR="$(cat /tmp/address.txt)"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  meta {
    consul-wan-federation = "1"
  }
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container containerPort and wanAddress.port can be changed" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.containerPort=8888' \
      --set 'meshGateway.wanAddress.source=NodeIP' \
      --set 'meshGateway.wanAddress.port=9999' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='WAN_ADDR="${HOST_IP}"
WAN_PORT="9999"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8888
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8888
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8888"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container wanAddress.source=NodeIP" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=NodeIP' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='WAN_ADDR="${HOST_IP}"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container wanAddress.source=NodeName" {
  cd `chart_dir`
  local obj=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=NodeName' \
      . | tee /dev/stderr)

  local actual=$(echo "$obj" |
      yq -r '.spec.template.spec.containers[0].env | map(select(.name == "NODE_NAME")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$obj" |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='WAN_ADDR="${NODE_NAME}"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container wanAddress.source=Static fails if wanAddress.static is empty" {
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

@test "meshGateway/Deployment: service-init init container wanAddress.source=Static" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Static' \
      --set 'meshGateway.wanAddress.static=example.com' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='WAN_ADDR="example.com"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container wanAddress.source=Service fails if service.enable is false" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.service.enabled=false' \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "if meshGateway.wanAddress.source=Service then meshGateway.service.enabled must be set to true" ]]
}

@test "meshGateway/Deployment: service-init init container wanAddress.source=Service, type=LoadBalancer" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.wanAddress.port=ignored' \
      --set 'meshGateway.service.enabled=true' \
      --set 'meshGateway.service.type=LoadBalancer' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='consul-k8s service-address \
  -k8s-namespace=default \
  -name=release-name-consul-mesh-gateway \
  -output-file=/tmp/address.txt
WAN_ADDR="$(cat /tmp/address.txt)"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container wanAddress.source=Service, type=NodePort" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.service.enabled=true' \
      --set 'meshGateway.service.nodePort=9999' \
      --set 'meshGateway.service.type=NodePort' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='WAN_ADDR="${HOST_IP}"
WAN_PORT="9999"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}

@test "meshGateway/Deployment: service-init init container wanAddress.source=Service, type=NodePort fails if service.nodePort is null" {
  cd `chart_dir`
  run helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.service.enabled=true' \
      --set 'meshGateway.service.type=NodePort' \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "if meshGateway.wanAddress.source=Service and meshGateway.service.type=NodePort, meshGateway.service.nodePort must be set" ]]
}

@test "meshGateway/Deployment: service-init init container wanAddress.source=Service, type=ClusterIP" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.wanAddress.source=Service' \
      --set 'meshGateway.wanAddress.port=ignored' \
      --set 'meshGateway.service.enabled=true' \
      --set 'meshGateway.service.type=ClusterIP' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='consul-k8s service-address \
  -k8s-namespace=default \
  -name=release-name-consul-mesh-gateway \
  -output-file=/tmp/address.txt
WAN_ADDR="$(cat /tmp/address.txt)"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "mesh-gateway"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}
@test "meshGateway/Deployment: service-init init container consulServiceName can be changed" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.consulServiceName=new-name' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers | map(select(.name == "service-init"))[0] | .command[2]' | tee /dev/stderr)

  exp='consul-k8s service-address \
  -k8s-namespace=default \
  -name=release-name-consul-mesh-gateway \
  -output-file=/tmp/address.txt
WAN_ADDR="$(cat /tmp/address.txt)"
WAN_PORT="443"

cat > /consul/service/service.hcl << EOF
service {
  kind = "mesh-gateway"
  name = "new-name"
  port = 8443
  address = "${POD_IP}"
  tagged_addresses {
    lan {
      address = "${POD_IP}"
      port = 8443
    }
    wan {
      address = "${WAN_ADDR}"
      port = ${WAN_PORT}
    }
  }
  checks = [
    {
      name = "Mesh Gateway Listening"
      interval = "10s"
      tcp = "${POD_IP}:8443"
      deregister_critical_service_after = "6h"
    }
  ]
}
EOF

/consul-bin/consul services register \
  /consul/service/service.hcl'

  [ "${actual}" = "${exp}" ]
}
