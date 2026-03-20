#!/usr/bin/env bats

load _helpers

@test "tlsroutes/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-tlsroutes-external.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "tlsroutes/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-tlsroutes-external.yaml \
        --set 'connectInject.enabled=false' \
        . 
}

@test "tlsroutes/CustomResourceDefinition: disabled with connectInject.apiGateway.manageExternalCRDs=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-tlsroutes-external.yaml \
        --set 'connectInject.apiGateway.manageExternalCRDs=false' \
        . 
}
