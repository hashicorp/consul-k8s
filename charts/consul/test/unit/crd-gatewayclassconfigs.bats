#!/usr/bin/env bats

load _helpers

@test "gatewayclassconfigs/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-gatewayclassconfigs.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gatewayclassconfigs/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-gatewayclassconfigs.yaml \
        --set 'connectInject.enabled=false' \
        . 
}
