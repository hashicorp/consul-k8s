#!/usr/bin/env bats

load _helpers

@test "tcproutes/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-tcproutes.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "tcproutes/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-tcproutes.yaml \
        --set 'connectInject.enabled=false' \
        . 
}

@test "tcproutes/CustomResourceDefinition: disabled with connectInject.apiGateway.manageExternalCRDs=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-tcproutes.yaml \
        --set 'connectInject.apiGateway.manageExternalCRDs=false' \
        . 
}
