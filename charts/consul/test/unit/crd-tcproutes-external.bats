#!/usr/bin/env bats

load _helpers

@test "tcproutes/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-tcproutes-external.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "tcproutes/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-tcproutes-external.yaml \
        --set 'connectInject.enabled=false' \
        . 
}

@test "tcproutes/CustomResourceDefinition: disabled with connectInject.apiGateway.manageExternalCRDs=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-tcproutes-external.yaml \
        --set 'connectInject.apiGateway.manageExternalCRDs=false' \
        . 
}

@test "tcproutes/CustomResourceDefinition: disabled with connectInject.apiGateway.manageExternalCRDs=false and connectInject.apiGateway.manageNonStandardCRDs=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-tcproutes-external.yaml \
        --set 'connectInject.apiGateway.manageExternalCRDs=false' \
        --set 'connectInject.apiGateway.manageNonStandardCRDs=false' \
        . 
}

@test "tcproutes/CustomResourceDefinition: enabled with connectInject.apiGateway.manageNonStandardCRDs=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-tcproutes-external.yaml \
        --set 'connectInject.apiGateway.manageNonStandardCRDs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
    [ "${actual}" = "true" ]
}
