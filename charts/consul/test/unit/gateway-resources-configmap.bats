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
# Mesh Gateway logLevel configuration

@test "gateway-resources/ConfigMap: Mesh Gateway logLevel default configuration" {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.container.consul.logging.level')
    [ "${actual}" = 'info' ]

    local actual=$(echo "$config" | yq -r '.initContainer.consul.logging.level')
    [ "${actual}" = 'info' ]
}


#--------------------------------------------------------------------
# API Gateway logLevel configuration

@test "gateway-resources/ConfigMap: API Gateway logLevel default configuration" {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'meshGateway.enabled=false' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.container.consul.logging.level')
    [ "${actual}" = 'info' ]

    local actual=$(echo "$config" | yq -r '.initContainer.consul.logging.level')
    [ "${actual}" = 'info' ]
}



@test "gateway-resources/ConfigMap: Mesh Gateway logLevel custom global configuration" {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=debug' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.container.consul.logging.level')
    [ "${actual}" = 'debug' ]

    local actual=$(echo "$config" | yq -r '.initContainer.consul.logging.level')
    [ "${actual}" = 'debug' ]
}

@test "gateway-resources/ConfigMap: Mesh Gateway logLevel custom meshGateway configuration" {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'meshGateway.logLevel=debug' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.container.consul.logging.level')
    [ "${actual}" = 'debug' ]

    local actual=$(echo "$config" | yq -r '.initContainer.consul.logging.level')
    [ "${actual}" = 'debug' ]
}

@test "gateway-resources/ConfigMap: Mesh Gateway logLevel custom meshGateway configuration overrides global configuration" {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        --set 'meshGateway.logLevel=debug' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.container.consul.logging.level')
    [ "${actual}" = 'debug' ]

    local actual=$(echo "$config" | yq -r '.initContainer.consul.logging.level')
    [ "${actual}" = 'debug' ]
}

#--------------------------------------------------------------------
# Mesh Gateway Extra Labels configuration

@test "gateway-resources/ConfigMap: Mesh Gateway gets Extra Labels when set" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.extraLabels.foo'='bar' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment.labels.set.foo' | tee /dev/stderr
    )
    [ "$actual" = 'bar' ]
}

#--------------------------------------------------------------------
# Mesh Gateway annotations configuration

@test "gateway-resources/ConfigMap: Mesh Gateway gets annotations when set" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'meshGateway.annotations.foo'='bar' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment.annotations.set.foo' | tee /dev/stderr
    )
    [ "$actual" = 'bar' ]
}

#--------------------------------------------------------------------
# Mesh Gateway WAN Address configuration

@test "gateway-resources/ConfigMap: Mesh Gateway WAN Address default annotations" {
    cd `chart_dir`
    local annotations=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.meshGateways[0].metadata.annotations' | tee /dev/stderr)

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]')
    [ "${actual}" = 'Service' ]

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-port"]')
    [ "${actual}" = '443' ]

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-address-static"]')
    [ "${actual}" = '' ]
}


@test "gateway-resources/ConfigMap: Mesh Gateway WAN Address NodePort annotations" {
    cd `chart_dir`
    local annotations=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'meshGateway.wanAddress.source=Service' \
        --set 'meshGateway.service.type=NodePort' \
        --set 'meshGateway.service.nodePort=30000' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.meshGateways[0].metadata.annotations' | tee /dev/stderr)

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]')
    [ "${actual}" = 'Service' ]

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-port"]')
    [ "${actual}" = '30000' ]

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-address-static"]')
    [ "${actual}" = '' ]
}

@test "gateway-resources/ConfigMap: Mesh Gateway WAN Address static configuration" {
    cd `chart_dir`
    local annotations=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'meshGateway.enabled=true' \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'meshGateway.wanAddress.source=Static' \
        --set 'meshGateway.wanAddress.static=127.0.0.1' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.meshGateways[0].metadata.annotations' | tee /dev/stderr)

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-address-source"]')
    [ "${actual}" = 'Static' ]

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-port"]')
    [ "${actual}" = '443' ]

    local actual=$(echo "$annotations" | yq -r '.["consul.hashicorp.com/gateway-wan-address-static"]')
    [ "${actual}" = '127.0.0.1' ]
}

#--------------------------------------------------------------------
# API Gateway Tests mapPrivilageContainerPorts

@test "gateway-resources/ConfigMap: API Gateway mapPrivilageContainerPorts empty by default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.container.portModifier')

    [ "${actual}" = 'null' ]

    local actual=$(echo "$config" | yq -r '.initContainer.portModifier')

    [ "${actual}" = 'null' ]
}


@test "gateway-resources/ConfigMap: API Gateway mapPrivilageContainerPorts overrides default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        --set 'connectInject.apiGateway.managedGatewayClass.mapPrivilegedContainerPorts=80' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.container.portModifier')

    [ "${actual}" = '80' ]

    local actual=$(echo "$config" | yq -r '.initContainer.portModifier')

    [ "${actual}" = '80' ]
}

#--------------------------------------------------------------------
# API Gateway Tests deployment replicas

@test "gateway-resources/ConfigMap: API Gateway deploymentConfig overrides default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        --set 'connectInject.apiGateway.managedGatewayClass.deployment.defaultInstances=2' \
        --set 'connectInject.apiGateway.managedGatewayClass.deployment.maxInstances=3' \
        --set 'connectInject.apiGateway.managedGatewayClass.deployment.minInstances=1' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.replicas.default')
    [ "${actual}" = '2' ]

    local actual=$(echo "$config" | yq -r '.replicas.min')
    [ "${actual}" = '1' ]

    local actual=$(echo "$config" | yq -r '.replicas.max')
    [ "${actual}" = '3' ]
}

@test "gateway-resources/ConfigMap: API Gateway deploymentConfig default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.replicas.default')
    [ "${actual}" = '1' ]

    local actual=$(echo "$config" | yq -r '.replicas.min')
    [ "${actual}" = '1' ]

    local actual=$(echo "$config" | yq -r '.replicas.max')
    [ "${actual}" = '1' ]
}

#--------------------------------------------------------------------
# API Gateway Tests nodeSelector

@test "gateway-resources/ConfigMap: API Gateway nodeSelector overrides default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        --set 'connectInject.apiGateway.managedGatewayClass.nodeSelector=- key: value' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.nodeSelector[0].key')
    echo ${actual}

    [ "${actual}" = 'value' ]
}

@test "gateway-resources/ConfigMap: API Gateway nodeSelector default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.nodeSelector')
    [ "${actual}" = 'null' ]
}

#--------------------------------------------------------------------
# API Gateway Tests tolerations

@test "gateway-resources/ConfigMap: API Gateway tolerations overrides default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        --set 'connectInject.apiGateway.managedGatewayClass.tolerations=- key: value' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.tolerations[0].key')
    echo "${actual}"

    [ "${actual}" = 'value' ]
}



@test "gateway-resources/ConfigMap: API Gateway tolerations default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.deployment' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.tolerations')
    [ "${actual}" = 'null' ]
}


#--------------------------------------------------------------------
# API Gateway Tests copyAnnotations

@test "gateway-resources/ConfigMap: API Gateway copyAnnotations overrides default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        --set 'connectInject.apiGateway.managedGatewayClass.copyAnnotations.service.annotations=- annotation.name' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.annotations' | tee /dev/stderr)

    local actual=$(echo "$config" | yq -r '.service[0]')
    echo "${actual}"
    [ "${actual}" = 'annotation.name' ]
}

@test "gateway-resources/ConfigMap: API Gateway copyAnnotations default {
    cd `chart_dir`
    local config=$(helm template \
        -s $target \
        --set 'global.experiments[0]=resource-apis' \
        --set 'ui.enabled=false' \
        --set 'global.logLevel=error' \
        . | tee /dev/stderr |
        yq -r '.data["config.yaml"]' | yq -r '.gatewayClassConfigs[0].spec.annotations' | tee /dev/stderr)

    local actual=$(echo "$config" | jq -r '.service')
    [ "${actual}" = 'null' ]
}


#--------------------------------------------------------------------
# TODO openShiftSSCName