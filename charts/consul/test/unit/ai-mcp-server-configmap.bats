#!/usr/bin/env bats

load _helpers

target=templates/ai-mcp-server-configmap.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/McpConfigMap: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/McpConfigMap: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/McpConfigMap: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# data

@test "ai/McpConfigMap: default values in config.json" {
    cd `chart_dir`
    local json=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.data["config.json"]' | tee /dev/stderr)

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['interceptorPort'])")
    [ "$actual" = "21102" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['transport'])")
    [ "$actual" = "streamable-http" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['path'])")
    [ "$actual" = "/mcp" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['protocolVersion'])")
    [ "$actual" = "2025-03-26" ]
}

@test "ai/McpConfigMap: custom values reflected in config.json" {
    cd `chart_dir`
    local json=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.mcpServer.defaults.transport=sse' \
        --set 'ai.mcpServer.defaults.path=/custom-mcp' \
        . | tee /dev/stderr |
        yq '.data["config.json"]' | tee /dev/stderr)

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['transport'])")
    [ "$actual" = "sse" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['path'])")
    [ "$actual" = "/custom-mcp" ]
}
