#!/usr/bin/env bats

load _helpers

@test "meshGateway/Deployment: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "meshGateway/Deployment: enabled with meshGateway.enabled true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: fails if connectInject.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=false' \
      --set 'client.grpc=true' \
      --set 'client.grpc=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.enabled must be true" ]]
}

@test "meshGateway/Deployment: fails if client.grpc=false" {
  cd `chart_dir`
  run helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=false' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "client.grpc must be true" ]]
}

@test "meshGateway/Deployment: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.annotations=key: value' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "meshGateway/Deployment: replicas defaults to 2" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "meshGateway/Deployment: replicas can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.replicas=3' \
      . | tee /dev/stderr |
      yq -r '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}

@test "meshGateway/Deployment: affinity defaults to one per node" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].topologyKey' | tee /dev/stderr)
  [ "${actual}" = "kubernetes.io/hostname" ]
}

@test "meshGateway/Deployment: affinity can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.affinity=key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.affinity.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "meshGateway/Deployment: no tolerations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.tolerations=- key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "meshGateway/Deployment: hostNetwork can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.hostNetwork=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.hostNetwork' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/Deployment: no dnsPolicy by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.dnsPolicy' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: dnsPolicy can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.dnsPolicy=ClusterFirst' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.dnsPolicy' | tee /dev/stderr)
  [ "${actual}" = "ClusterFirst" ]
}

@test "meshGateway/Deployment: global.BootstrapACLs enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr )
  local init_container=$(echo "${actual}" | yq -r '.spec.template.spec.initContainers[1].name' | tee /dev/stderr)
  [ "${init_container}" = "mesh-gateway-acl-init" ]

  local secret=$(echo "${actual}" | yq -r '.spec.template.spec.containers[0].env[3].name' | tee /dev/stderr)
  [ "${secret}" = "CONSUL_HTTP_TOKEN" ]
}

@test "meshGateway/Deployment: envoy image has default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "envoyproxy/envoy:v1.10.0" ]
}

@test "meshGateway/Deployment: envoy image can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.imageEnvoy=new/image' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "new/image" ]
}

@test "meshGateway/Deployment: resources has default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources' | tee /dev/stderr)

  [ $(echo "${actual}" | yq -r '.requests.memory') = "128Mi" ]
  [ $(echo "${actual}" | yq -r '.requests.cpu') = "250m" ]
  [ $(echo "${actual}" | yq -r '.limits.memory') = "256Mi" ]
  [ $(echo "${actual}" | yq -r '.limits.cpu') = "500m" ]
}

@test "meshGateway/Deployment: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.resources=requests: yadayada' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.requests' | tee /dev/stderr)
  [ "${actual}" = "yadayada" ]
}

@test "meshGateway/Deployment: containerPort defaults to 443" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr \
      | yq  '.spec.template.spec.containers[0]' | tee /dev/stderr)

  [[ $(echo "$actual" | yq -r '.command[2]')  =~ '-address="${POD_IP}:443"' ]]
  [ $(echo "$actual" | yq -r '.ports[0].containerPort')  = "443" ]
  [ $(echo "$actual" | yq -r '.livenessProbe.tcpSocket.port')  = "443" ]
  [ $(echo "$actual" | yq -r '.readinessProbe.tcpSocket.port')  = "443" ]
}

@test "meshGateway/Deployment: containerPort can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.containerPort=8443' \
      . | tee /dev/stderr \
      | yq  '.spec.template.spec.containers[0]' | tee /dev/stderr)

  [[ $(echo "$actual" | yq -r '.command[2]')  =~ '-address="${POD_IP}:8443"' ]]
  [ $(echo "$actual" | yq -r '.ports[0].containerPort')  = "8443" ]
  [ $(echo "$actual" | yq -r '.livenessProbe.tcpSocket.port')  = "8443" ]
  [ $(echo "$actual" | yq -r '.readinessProbe.tcpSocket.port')  = "8443" ]
}

@test "meshGateway/Deployment: wanAddress.port defaults to 443" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.wanAddress.useNodeIP=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  [[ "${actual}" =~ '-wan-address="${HOST_IP}:443"' ]]
}

@test "meshGateway/Deployment: wanAddress uses NodeIP by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  [[ "${actual}" =~ '-wan-address="${HOST_IP}:443"' ]]
}

@test "meshGateway/Deployment: wanAddress.useNodeIP" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.wanAddress.useNodeIP=true' \
      --set 'meshGateway.wanAddress.port=4444' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  [[ "${actual}" =~ '-wan-address="${HOST_IP}:4444"' ]]
}

@test "meshGateway/Deployment: wanAddress.useNodeName" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.wanAddress.useNodeIP=false' \
      --set 'meshGateway.wanAddress.useNodeName=true' \
      --set 'meshGateway.wanAddress.port=4444' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  [[ "${actual}" =~ '-wan-address="${NODE_NAME}:4444"' ]]
}

@test "meshGateway/Deployment: wanAddress.host" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.wanAddress.useNodeIP=false' \
      --set 'meshGateway.wanAddress.useNodeName=false' \
      --set 'meshGateway.wanAddress.host=myhost' \
      --set 'meshGateway.wanAddress.port=4444' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)
  [[ "${actual}" =~ '-wan-address="myhost:4444"' ]]
}

@test "meshGateway/Deployment: fails if consulServiceName is set and bootstrapACLs is true" {
  cd `chart_dir`
  run helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.consulServiceName=override' \
      --set 'global.bootstrapACLs=true' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "if global.bootstrapACLs is true, meshGateway.consulServiceName cannot be set" ]]
}

@test "meshGateway/Deployment: consulServiceName can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.consulServiceName=overridden' \
      . | tee /dev/stderr \
      | yq '.spec.template.spec.containers[0]' | tee /dev/stderr )

  [[ $(echo "${actual}" | yq -r '.command[2]' ) =~ '-service="overridden"' ]]
  [[ $(echo "${actual}" | yq -r '.lifecycle.preStop.exec.command' ) =~ '-id=\"overridden\"' ]]
}

@test "meshGateway/Deployment: healthchecks are on by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr \
      | yq '.spec.template.spec.containers[0]' | tee /dev/stderr )

  local liveness=$(echo "${actual}" | yq -r '.livenessProbe | length > 0' | tee /dev/stderr)
  [ "${liveness}" = "true" ]
  local readiness=$(echo "${actual}" | yq -r '.readinessProbe | length > 0' | tee /dev/stderr)
  [ "${readiness}" = "true" ]
}

@test "meshGateway/Deployment: can disable healthchecks" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.enableHealthChecks=false' \
      . | tee /dev/stderr \
      | yq '.spec.template.spec.containers[0]' | tee /dev/stderr )

  local liveness=$(echo "${actual}" | yq -r '.livenessProbe | length > 0' | tee /dev/stderr)
  [ "${liveness}" = "false" ]
  local readiness=$(echo "${actual}" | yq -r '.readinessProbe | length > 0' | tee /dev/stderr)
  [ "${readiness}" = "false" ]
}

@test "meshGateway/Deployment: no hostPort by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].ports[0].hostPort' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: can set a hostPort" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.hostPort=443' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].ports[0].hostPort' | tee /dev/stderr)

  [ "${actual}" = "443" ]
}

@test "meshGateway/Deployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: can set a priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.priorityClassName=name' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "name" ]
}

@test "meshGateway/Deployment: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "meshGateway/Deployment: can set a nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-deployment.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'meshGateway.nodeSelector=key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector.key' | tee /dev/stderr)

  [ "${actual}" = "value" ]
}
