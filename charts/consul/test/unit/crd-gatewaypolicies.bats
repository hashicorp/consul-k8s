#!/usr/bin/env bats

load _helpers

@test "gatewaypolicies/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-gatewaypolicies.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gatewaypolicies/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-gatewaypolicies.yaml \
        --set 'connectInject.enabled=false' \
        .
}
