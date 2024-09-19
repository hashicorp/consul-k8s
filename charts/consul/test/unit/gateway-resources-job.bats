#!/usr/bin/env bats

load _helpers

target=templates/gateway-resources-job.yaml

@test "gatewayresources/Job: fails if .values.apiGateway is set" {
  cd $(chart_dir)
  run helm template \
    -s templates/tests/test-runner.yaml \
    --set 'apiGateway.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "[DEPRECATED and REMOVED] the apiGateway stanza is no longer supported as of Consul 1.19.0. Use connectInject.apiGateway instead." ]]
}

@test "gatewayresources/Job: enabled by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    . | tee /dev/stderr |
    yq 'length > 0' | tee /dev/stderr)
  [ "$actual" = "true" ]
}

@test "gatewayresources/Job: disabled with connectInject.enabled=false" {
  cd $(chart_dir)
  assert_empty helm template \
    -s $target \
    --set 'connectInject.enabled=false' \
    .
}

@test "gatewayresources/Job: imageK8S set properly" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'global.imageK8S=foo' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].image == "foo"' | tee /dev/stderr)
  [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# configuration

@test "gatewayresources/Job: default configuration" {
  cd $(chart_dir)
  local spec=$(helm template \
    -s $target \
    . | tee /dev/stderr)
  # yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-deployment-default-instances=1"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-deployment-max-instances=1"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-deployment-min-instances=1"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-service-type=LoadBalancer"])')
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: custom configuration blah" {
  cd $(chart_dir)
  local spec=$(helm template \
    -s $target \
    --set 'connectInject.apiGateway.managedGatewayClass.deployment.defaultInstances=2' \
    --set 'connectInject.apiGateway.managedGatewayClass.deployment.minInstances=1' \
    --set 'connectInject.apiGateway.managedGatewayClass.deployment.maxInstances=3' \
    --set 'connectInject.apiGateway.managedGatewayClass.nodeSelector=foo: bar' \
    --set 'connectInject.apiGateway.managedGatewayClass.copyAnnotations.service.annotations=- bingo' \
    --set 'connectInject.apiGateway.managedGatewayClass.serviceType=Foo' \
    --set 'connectInject.apiGateway.managedGatewayClass.openshiftSCCName=hello' \
    . | tee /dev/stderr)
  # yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-deployment-default-instances=2"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-deployment-max-instances=3"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-deployment-min-instances=1"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-service-type=Foo"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-node-selector", "foo: bar"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-service-annotations", "- bingo"])')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-service-type=Foo"])')
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: custom configuration openshift enabled" {
  cd $(chart_dir)
  local spec=$(helm template \
    -s $target \
    --set 'global.openshift.enabled=true' \
    --set 'connectInject.apiGateway.managedGatewayClass.openshiftSCCName=hello' \
    . | tee /dev/stderr)
  # yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$spec" | yq '.spec.template.spec.containers[0].args | contains(["-openshift-scc-name=hello"])')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# annotations

@test "gatewayresources/Job: no annotations defined by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata.annotations |
        del(."consul.hashicorp.com/connect-inject") |
        del(."consul.hashicorp.com/mesh-inject") |
        del(."consul.hashicorp.com/config-checksum")' |
    tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

#--------------------------------------------------------------------
# tolerations

@test "apiGateway/GatewayClassConfig: tolerations" {
  cd $(chart_dir)
  local tolerations=$(helm template \
    -s templates/gateway-resources-job.yaml \
    --set 'connectInject.apiGateway.managedGatewayClass.tolerations=- "operator": "Equal" \
"effect": "NoSchedule" \
"key": "node" \
"value": "clients"' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)
  echo "TOLERATIONS: ${tolerations}"
  local actual=$(echo $tolerations | yq 'contains(["tolerations","- \"operator\": \"Equal\" \n\"effect\": \"NoSchedule\" \n\"key\": \"node\" \n\"value\": \"clients\"" ])')
  [ "${actual}" = "true" ]
}
