#!/usr/bin/env bats

load _helpers

target=templates/gateway-resources-configmap.yaml

@test "gateway-resources/ConfigMap: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=false' \
        .
}

@test "gateway-resources/ConfigMap: enabled with connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gateway-resources/ConfigMap: contains resources configuration as JSON" {
    cd `chart_dir`
    local resources=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'connectInject.apiGateway.managedGatewayClass.resources.requests.memory=200Mi' \
        --set 'connectInject.apiGateway.managedGatewayClass.resources.requests.cpu=200m' \
        --set 'connectInject.apiGateway.managedGatewayClass.resources.limits.memory=220Mi' \
        --set 'connectInject.apiGateway.managedGatewayClass.resources.limits.cpu=220m' \
        . | tee /dev/stderr |
        yq '.data["resources.json"] | fromjson' | tee /dev/stderr)

    local actual=$(echo $resources | jq -r '.requests.memory')
    [ $actual = '200Mi' ]

    local actual=$(echo $resources | jq -r '.requests.cpu')
    [ $actual = '200m' ]

    local actual=$(echo $resources | jq -r '.limits.memory')
    [ $actual = '220Mi' ]

    local actual=$(echo $resources | jq -r '.limits.cpu')
    [ $actual = '220m' ]
}

@test "gateway-resources/ConfigMap: does not contain config.yaml resources" {
    cd `chart_dir`
    local resources=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ui.enabled=false' \
        . | tee /dev/stderr |
        yq '.data["config.yaml"]' | tee /dev/stderr)
    [ $resources = null ]

}

#--------------------------------------------------------------------
# TODO openShiftSSCName