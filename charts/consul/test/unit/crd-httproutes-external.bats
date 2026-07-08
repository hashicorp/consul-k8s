#!/usr/bin/env bats

load _helpers

@test "httproutes/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-httproutes-external.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "httproutes/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-httproutes-external.yaml \
        --set 'connectInject.enabled=false' \
        . 
}

@test "httproutes/CustomResourceDefinition: disabled with connectInject.apiGateway.manageExternalCRDs=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-httproutes-external.yaml \
        --set 'connectInject.apiGateway.manageExternalCRDs=false' \
        . 
}

@test "httproutes/CRD tombstone: renders with resource-policy:keep on OCP when isOcpGreaterthan4_18=true and installK8sNetworkingCRDs=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-httproutes-external.yaml \
        --set "global.openshift.enabled=true" \
        --set "global.openshift.isOcpGreaterthan4_18=true" \
        --set "global.installK8sNetworkingCRDs=false" \
        . | tee /dev/stderr |
        yq "select(.metadata.name == \"httproutes.gateway.networking.k8s.io\")
            | .metadata.annotations[\"helm.sh/resource-policy\"]")
    [ "$actual" = "keep" ]
}

@test "httproutes/CRD tombstone: does NOT render when openshift.enabled=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-httproutes-external.yaml \
        --set "global.openshift.enabled=false" \
        --set "global.installK8sNetworkingCRDs=false" \
        . | tee /dev/stderr |
        yq "select(.metadata.annotations[\"helm.sh/resource-policy\"] == \"keep\") | .metadata.name")
    [ -z "$actual" ]
}

