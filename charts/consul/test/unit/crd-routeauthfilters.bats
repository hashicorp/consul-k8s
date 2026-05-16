#!/usr/bin/env bats

load _helpers

@test "routeauth-filters/CustomResourceDefinition: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-routeauthfilters.yaml \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "routeauth-filter/CustomResourceDefinition: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-routeauthfilters.yaml \
        --set 'connectInject.enabled=false' \
        .
}
