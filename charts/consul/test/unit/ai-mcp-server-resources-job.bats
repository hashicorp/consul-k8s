#!/usr/bin/env bats

load _helpers

target=templates/ai-mcp-server-resources-job.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/McpJob: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/McpJob: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/McpJob: fails validation when connectInject.enabled=false" {
    cd `chart_dir`
    run helm template \
        -s $target \
        --set 'ai.enabled=true' \
        --set 'connectInject.enabled=false' \
        --set 'global.enabled=false' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "requires connectInject.enabled=true" ]]
}

@test "ai/McpJob: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq -r 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# hook annotations

@test "ai/McpJob: has post-install,post-upgrade hook annotation" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq -r '.metadata.annotations["helm.sh/hook"]' | tee /dev/stderr)
    [ "$actual" = "post-install,post-upgrade" ]
}

#--------------------------------------------------------------------
# image

@test "ai/McpJob: uses global.imageK8S" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'global.imageK8S=my-image:1.0' \
        . | tee /dev/stderr |
        yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
    [ "$actual" = "my-image:1.0" ]
}

#--------------------------------------------------------------------
# args

@test "ai/McpJob: default args passed to subcommand" {
    cd `chart_dir`
    local args=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

    local actual=$(echo "$args" | yq -r 'contains(["mcp-server-resources"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq -r 'contains(["-transport=streamable-http"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq -r 'contains(["-path=/mcp"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq -r 'contains(["-interceptor-port=21102"])')
    [ "$actual" = "true" ]
}

@test "ai/McpJob: custom mcpServer values passed through" {
    cd `chart_dir`
    local args=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.mcpServer.defaults.transport=sse' \
        --set 'ai.mcpServer.defaults.path=/custom' \
        --set 'ai.mcpServer.defaults.protocolVersion=2024-11-05' \
        . | tee /dev/stderr |
        yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

    local actual=$(echo "$args" | yq -r 'contains(["-transport=sse"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq -r 'contains(["-path=/custom"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq -r 'contains(["-protocol-version=2024-11-05"])')
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# mesh injection disabled

@test "ai/McpJob: consul mesh inject annotations set to false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq -r '.spec.template.metadata.annotations["consul.hashicorp.com/connect-inject"]' | tee /dev/stderr)
    [ "$actual" = "false" ]
}
