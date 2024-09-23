#!/usr/bin/env bats

load _helpers

target=templates/gateway-resources-job.yaml

@test "gatewayresources/Job: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gatewayresources/Job: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=false' \
        .
}

@test "gatewayresources/Job: imageK8S set properly" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set 'global.imageK8S=foo' \
        . | tee /dev/stderr |
        yq '.spec.template.spec.containers[0].image == "foo"' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# fallback configuration
# to be removed in 1.17 (t-eckert 2023-05-23)

@test "gatewayresources/Job: fallback configuration is used when apiGateway.enabled is true" {
  cd `chart_dir`
  local spec=$(helm template \
      -s $target  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=testing' \
      --set 'apiGateway.managedGatewayClass.nodeSelector=foo: bar' \
      --set 'apiGateway.managedGatewayClass.tolerations=- key: bar' \
      --set 'apiGateway.managedGatewayClass.copyAnnotations.service.annotations=- bingo' \
      --set 'apiGateway.managedGatewayClass.serviceType=LoadBalancer' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$spec" | jq '.[9] | ."-node-selector=foo"')
  [ "${actual}" = "\"bar\"" ]

  local actual=$(echo "$spec" | jq '.[10] | ."-tolerations=- key"')
  [ "${actual}" = "\"bar\"" ]

  local actual=$(echo "$spec" | jq '.[11]')
  [ "${actual}" = "\"-service-annotations=- bingo\"" ]
}

#--------------------------------------------------------------------
# configuration

@test "gatewayresources/Job: default configuration" {
  cd `chart_dir`
  local spec=$(helm template \
      -s $target  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$spec" | jq 'any(index("-deployment-default-instances=1"))')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | jq 'any(index("-deployment-max-instances=1"))')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | jq 'any(index("-deployment-min-instances=1"))')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | jq 'any(index("-service-type=LoadBalancer"))')
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: custom configuration" {
  cd `chart_dir`
  local spec=$(helm template \
      -s $target  \
      --set 'connectInject.apiGateway.managedGatewayClass.deployment.defaultInstances=2' \
      --set 'connectInject.apiGateway.managedGatewayClass.deployment.minInstances=1' \
      --set 'connectInject.apiGateway.managedGatewayClass.deployment.maxInstances=3' \
      --set 'connectInject.apiGateway.managedGatewayClass.nodeSelector=foo: bar' \
      --set 'connectInject.apiGateway.managedGatewayClass.copyAnnotations.service.annotations=- bingo' \
      --set 'connectInject.apiGateway.managedGatewayClass.serviceType=Foo' \
      --set 'connectInject.apiGateway.managedGatewayClass.openshiftSCCName=hello' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$spec" | jq 'any(index("-deployment-default-instances=2"))')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | jq 'any(index("-deployment-max-instances=3"))')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | jq 'any(index("-deployment-min-instances=1"))')
  [ "${actual}" = "true" ]

  local actual=$(echo "$spec" | jq 'any(index("-service-type=Foo"))')
  [ "${actual}" = "true" ]

  local actual=$(echo $spec | yq 'contains(["-node-selector", "foo: bar"])')
  [ "${actual}" = "true" ]

  local actual=$(echo $spec | yq 'contains(["-service-annotations", "- bingo"])')
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClassConfig: custom configuration openshift enabled" {
  cd `chart_dir`
  local spec=$(helm template \
      -s $target  \
      --set 'global.openshift.enabled=true' \
      --set 'connectInject.apiGateway.managedGatewayClass.openshiftSCCName=hello' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$spec" | jq '.[13]')
  [ "${actual}" = "\"-openshift-scc-name=hello\"" ]
}


#--------------------------------------------------------------------
# annotations

@test "gatewayresources/Job: no annotations defined by default" {
  cd `chart_dir`
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
  cd `chart_dir`
  local tolerations=$(helm template \
      -s $target  \
      --set 'connectInject.apiGateway.managedGatewayClass.tolerations=- "operator": "Equal" \
"effect": "NoSchedule" \
"key": "node" \
"value": "clients" \
- "operator": "Equal" \
"effect": "NoSchedule" \
"key": "node2" \
"value": "clients2"' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo $tolerations | yq 'contains(["tolerations","- \"operator\": \"Equal\" \n\"effect\": \"NoSchedule\" \n\"key\": \"node\" \n\"value\": \"clients\" \n- \"operator\": \"Equal\" \n\"effect\": \"NoSchedule\" \n\"key\": \"node2\" \n\"value\": \"clients2\"" ])')
  [ "${actual}" = "true" ]
}