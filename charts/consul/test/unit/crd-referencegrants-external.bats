#!/usr/bin/env bats

load _helpers

@test "referencegrants/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-referencegrants-external.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "referencegrants/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-referencegrants-external.yaml \
        --set 'connectInject.enabled=false' \
        .
}

@test "referencegrants/CustomResourceDefinition: disabled with connectInject.apiGateway.manageExternalCRDs=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-referencegrants-external.yaml \
        --set 'connectInject.apiGateway.manageExternalCRDs=false' \
        .
}

@test "referencegrants/CRD tombstone: renders with resource-policy:keep on OCP when isOcpGreaterthan4_18=true and installK8sNetworkingCRDs=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-referencegrants-external.yaml \
        --set "global.openshift.enabled=true" \
        --set "global.openshift.isOcpGreaterthan4_18=true" \
        --set "global.installK8sNetworkingCRDs=false" \
        . | tee /dev/stderr |
        yq "select(.metadata.name == \"referencegrants.gateway.networking.k8s.io\")
            | .metadata.annotations[\"helm.sh/resource-policy\"]")
    [ "$actual" = "keep" ]
}

@test "referencegrants/CRD tombstone: does NOT render when openshift.enabled=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-referencegrants-external.yaml \
        --set "global.openshift.enabled=false" \
        --set "global.installK8sNetworkingCRDs=false" \
        . | tee /dev/stderr |
        yq "select(.metadata.annotations[\"helm.sh/resource-policy\"] == \"keep\") | .metadata.name")
    [ -z "$actual" ]
}

@test "referencegrants/CRD tombstone: does NOT render when primary block is active (no duplicate)" {
    cd `chart_dir`
    local count=$(helm template \
        -s templates/crd-referencegrants-external.yaml \
        --set "global.openshift.enabled=true" \
        --set "global.openshift.isOcpGreaterthan4_18=false" \
        --set "global.installK8sNetworkingCRDs=true" \
        . | tee /dev/stderr |
        yq "select(.metadata.name == \"referencegrants.gateway.networking.k8s.io\")" | grep -c "^---")
    [ "$count" -eq 1 ]
}
