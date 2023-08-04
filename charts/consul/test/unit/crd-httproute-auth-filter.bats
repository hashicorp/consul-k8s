#!/usr/bin/env bats

load _helpers

@test "httproute-auth-filters/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-http-route-auth-filter.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "httproute-auth-filter/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-http-route-auth-filter.yaml \
        --set 'connectInject.enabled=false' \
        .
}
