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
