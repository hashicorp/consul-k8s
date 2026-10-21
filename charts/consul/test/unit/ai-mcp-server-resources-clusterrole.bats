#!/usr/bin/env bats

load _helpers

target=templates/ai-mcp-server-resources-clusterrole.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/McpClusterRole: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/McpClusterRole: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/McpClusterRole: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# rules

@test "ai/McpClusterRole: grants get, create, update on mcpserverconfigs" {
    cd `chart_dir`
    local rules=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.rules[] | select(.resources[] == "mcpserverconfigs")' | tee /dev/stderr)

    local actual=$(echo "$rules" | yq '.verbs | contains(["get"])' | tee /dev/stderr)
    [ "$actual" = "true" ]

    local actual=$(echo "$rules" | yq '.verbs | contains(["create"])' | tee /dev/stderr)
    [ "$actual" = "true" ]

    local actual=$(echo "$rules" | yq '.verbs | contains(["update"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}
