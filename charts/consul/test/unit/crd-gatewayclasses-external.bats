#!/usr/bin/env bats

load _helpers

@test "gatewayclasses/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-gatewayclasses-external.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gatewayclasses/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-gatewayclasses-external.yaml \
        --set 'connectInject.enabled=false' \
        . 
}

@test "gatewayclasses/CustomResourceDefinition: disabled with connectInject.apiGateway.manageExternalCRDs=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-gatewayclasses-external.yaml \
        --set 'connectInject.apiGateway.manageExternalCRDs=false' \
        . 
}

@test "gatewayclasses/CRD tombstone: renders with resource-policy:keep on OCP when isOcpGreaterthan4_18=true and installK8sNetworkingCRDs=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-gatewayclasses-external.yaml \
        --set "global.openshift.enabled=true" \
        --set "global.openshift.isOcpGreaterthan4_18=true" \
        --set "global.installK8sNetworkingCRDs=false" \
        . | tee /dev/stderr |
        yq "select(.metadata.name == \"gatewayclasses.gateway.networking.k8s.io\")
            | .metadata.annotations[\"helm.sh/resource-policy\"]")
    [ "$actual" = "keep" ]
}

@test "gatewayclasses/CRD tombstone: renders with resource-policy:keep on OCP when installK8sNetworkingCRDs=false and isOcpGreaterthan4_18=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-gatewayclasses-external.yaml \
        --set "global.openshift.enabled=true" \
        --set "global.openshift.isOcpGreaterthan4_18=false" \
        --set "global.installK8sNetworkingCRDs=false" \
        . | tee /dev/stderr |
        yq "select(.metadata.annotations[\"helm.sh/resource-policy\"] == \"keep\")
            | .metadata.name")
    [ "$actual" = "gatewayclasses.gateway.networking.k8s.io" ]
}

@test "gatewayclasses/CRD tombstone: does NOT render when openshift.enabled=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-gatewayclasses-external.yaml \
        --set "global.openshift.enabled=false" \
        --set "global.installK8sNetworkingCRDs=false" \
        . | tee /dev/stderr |
        yq "select(.metadata.annotations[\"helm.sh/resource-policy\"] == \"keep\") | .metadata.name")
    [ -z "$actual" ]
}

@test "gatewayclasses/CRD tombstone: does NOT render when primary block is active (no duplicate)" {
    cd `chart_dir`
    # When primary block renders, tombstone must be suppressed — only one copy of the CRD
    local count=$(helm template \
        -s templates/crd-gatewayclasses-external.yaml \
        --set "global.openshift.enabled=true" \
        --set "global.openshift.isOcpGreaterthan4_18=false" \
        --set "global.installK8sNetworkingCRDs=true" \
        . | tee /dev/stderr |
        yq "select(.metadata.name == \"gatewayclasses.gateway.networking.k8s.io\")" | grep -c "^---")
    [ "$count" -eq 1 ]
}

