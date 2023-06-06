#!/usr/bin/env bats

load _helpers

target=templates/gateway-cleanup-serviceaccount.yaml

@test "gatewaycleanup/ServiceAccount: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gatewaycleanup/ServiceAccount: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=false' \
        . 
}

