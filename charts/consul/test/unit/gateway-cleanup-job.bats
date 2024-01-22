#!/usr/bin/env bats

load _helpers

target=templates/gateway-cleanup-job.yaml

@test "gatewaycleanup/Job: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gatewaycleanup/Job: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=false' \
        .
}


#--------------------------------------------------------------------
# annotations

@test "gatewaycleanup/Job: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
        -s $target \
        . | tee /dev/stderr |
        yq -r '.spec.template.metadata.annotations |
        del(."consul.hashicorp.com/connect-inject") |
        del(."consul.hashicorp.com/mesh-inject") |
        del(."consul.hashicorp.com/config-checksum")' |
        tee /dev/stderr)
    [ "${actual}" = "{}" ]
}
