#!/usr/bin/env bats

load _helpers

target=templates/ai-mcp-server-resources-serviceaccount.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/McpServiceAccount: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/McpServiceAccount: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/McpServiceAccount: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/McpServiceAccount: can set imagePullSecrets" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'global.imagePullSecrets[0].name=my-secret' \
        . | tee /dev/stderr |
        yq '.imagePullSecrets[0].name' | tee /dev/stderr)
    [ "$actual" = "my-secret" ]
}
