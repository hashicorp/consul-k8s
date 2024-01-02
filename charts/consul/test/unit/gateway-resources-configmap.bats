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

@test "gateway-resources/ConfigMap: does not contain config.yaml resources without .global.experiments equal to resource-apis" {
    cd `chart_dir`
    local resources=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ui.enabled=false' \
        . | tee /dev/stderr |
        yq '.data["config.yaml"]' | tee /dev/stderr)
    [ $resources = null ]

}

@test "gateway-resources/ConfigMap: contains config.yaml resources with .global.experiments equal to resource-apis" {
    cd `chart_dir`
    local resources=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        . | tee /dev/stderr |
        yq '.data["config.yaml"]' | tee /dev/stderr)

    [ "$resources" != null ]
}


#--------------------------------------------------------------------
# Mesh Gateway WAN Address configuration

@test "gatewayresources/ConfigMap: Mesh Gateway WAN Address default annotations" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        . | tee /dev/stderr |
        yq '.data["config.yaml"]' | yq -r -j '.meshGateways[0].metadata.annotations' | tee /dev/stderr)

    local expected=$(echo '{
        "consul.hashicorp.com/gateway-wan-address-source": "Service",
        "consul.hashicorp.com/gateway-wan-port": "443",
        "consul.hashicorp.com/gateway-wan-address-static": ""
    }' | tee /dev/stderr)

    local equal=$(jq -n --argjson a "$actual" --argjson b "$expected" '$a == $b')
    [ "${equal}" = 'true' ]
}

@test "gatewayresources/ConfigMap: Mesh Gateway WAN Address NodePort annotations" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'meshGateway.wanAddress.source=Service' \
        --set 'meshGateway.service.type=NodePort' \
        --set 'meshGateway.service.nodePort=30000' \
        . | tee /dev/stderr |
        yq '.data["config.yaml"]' | yq -r -j '.meshGateways[0].metadata.annotations' | tee /dev/stderr)

    local expected=$(echo '{
        "consul.hashicorp.com/gateway-wan-address-source": "Service",
        "consul.hashicorp.com/gateway-wan-port": "30000",
        "consul.hashicorp.com/gateway-wan-address-static": ""
    }' | tee /dev/stderr)

    local equal=$(jq -n --argjson a "$actual" --argjson b "$expected" '$a == $b')
    [ "${equal}" = 'true' ]
}

@test "gatewayresources/ConfigMap: Mesh Gateway WAN Address static configuration" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'meshGateway.wanAddress.source=Static' \
        --set 'meshGateway.wanAddress.static=127.0.0.1' \
        . | tee /dev/stderr |
        yq '.data["config.yaml"]' | yq -r -j '.meshGateways[0].metadata.annotations' | tee /dev/stderr)

    local expected=$(echo '{
        "consul.hashicorp.com/gateway-wan-address-source": "Static",
        "consul.hashicorp.com/gateway-wan-port": "443",
        "consul.hashicorp.com/gateway-wan-address-static": "127.0.0.1"
    }' | tee /dev/stderr)

    local equal=$(jq -n --argjson a "$actual" --argjson b "$expected" '$a == $b')
    [ "${equal}" = 'true' ]
}

