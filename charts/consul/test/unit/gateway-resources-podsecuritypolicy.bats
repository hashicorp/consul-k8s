#!/usr/bin/env bats

load _helpers

target=templates/gateway-resources-podsecuritypolicy.yaml

@test "gatewayresources/PodSecurityPolicy: disabled by default" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=false' \
        . 
}

@test "gatewayresources/PodSecurityPolicy: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=false' \
        . 
}

@test "gatewayresources/PodSecurityPolicy: disabled with global.enablePodSecurityPolicies=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'global.enablePodSecurityPolicies=false' \
        . 
}


@test "gatewayresources/PodSecurityPolicy: enabled with connectInject.enabled=true and global.enablePodSecurityPolicies=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'global.enablePodSecurityPolicies=true' \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}
