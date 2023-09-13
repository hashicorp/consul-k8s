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

@test "tcproutes/CustomResourceDefinition: disabled with connectInject.apiGateway.manageExternalCRDs=false and connectInject.apiGateway.manageCustomCRDs=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-tcproutes.yaml \
        --set 'connectInject.apiGateway.manageExternalCRDs=false' \
        --set 'connectInject.apiGateway.manageCustomCRDs=false' \
        . 
}

@test "tcproutes/CustomResourceDefinition: enabled with connectInject.apiGateway.manageCustomCRDs=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-tcproutes.yaml \
        --set 'connectInject.apiGateway.manageCustomCRDs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
    [ "${actual}" = "true" ]
}
