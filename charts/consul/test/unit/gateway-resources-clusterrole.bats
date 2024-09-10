#!/usr/bin/env bats

load _helpers

target=templates/gateway-resources-clusterrole.yaml

@test "gatewayresources/ClusterRole: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gatewayresources/ClusterRole: enabled with global.rbac.create false" {
  cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'global.rbac.create=false'  \
        .
}
@test "gatewayresources/ClusterRole: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=false' \
        . 
}

@test "gatewayresources/ClusterRole: can use podsecuritypolicies with global.enablePodSecurityPolicy=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        --set "global.enablePodSecurityPolicies=true" \
        . | tee /dev/stderr |
        yq '.rules[] | select((.resources[0] == "podsecuritypolicies") and (.verbs[0] == "use")) | length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

