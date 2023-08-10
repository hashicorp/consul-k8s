#!/usr/bin/env bats

load _helpers

@test "gateway-resources/ConfigMap: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/gateway-resources-configmap.yaml \
        --set 'connectInject.enabled=false' \
        . 
}

@test "gateway-resources/ConfigMap: enabled with connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/gateway-resources-configmap.yaml \
        --set 'connectInject.enabled=true' \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gateway-resources/ConfigMap: contains resources configuration as JSON" {
    cd `chart_dir`
    local resources=$(helm template \
        -s templates/gateway-resources-configmap.yaml \
        --set 'connectInject.enabled=true' \
        --set 'connectInject.apiGateway.managedGatewayClass.resources.requests.memory=200Mi' \
        --set 'connectInject.apiGateway.managedGatewayClass.resources.requests.cpu=200m' \
        --set 'connectInject.apiGateway.managedGatewayClass.resources.limits.memory=220Mi' \
        --set 'connectInject.apiGateway.managedGatewayClass.resources.limits.cpu=220m' \
        . | tee /dev/stderr |
        yq '.data.["resources.json"]' | tee /dev/stderr)

    [ "$resources" = '{"limits":{"cpu":"220m","memory":"220Mi"},"requests":{"cpu":"200m","memory":"200Mi"}}' ]
}
