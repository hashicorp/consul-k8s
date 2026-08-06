#!/usr/bin/env bats

load _helpers

target=templates/ai-agent-resources-job.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/AgentJob: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/AgentJob: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/AgentJob: fails validation when connectInject.enabled=false" {
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

@test "ai/AgentJob: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# hook annotations

@test "ai/AgentJob: has post-install,post-upgrade hook annotation" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.annotations["helm.sh/hook"]' | tee /dev/stderr)
    [ "$actual" = "post-install,post-upgrade" ]
}

#--------------------------------------------------------------------
# image

@test "ai/AgentJob: uses global.imageK8S" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'global.imageK8S=agent-image:2.0' \
        . | tee /dev/stderr |
        yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
    [ "$actual" = "agent-image:2.0" ]
}

#--------------------------------------------------------------------
# args

@test "ai/AgentJob: default args passed to subcommand" {
    cd `chart_dir`
    local args=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

    local actual=$(echo "$args" | yq 'contains(["agent-resources"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-interceptor-port=21101"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-mcp-port=15101"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-hitl-port=16101"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-approval-timeout=60s"])')
    [ "$actual" = "true" ]
}

@test "ai/AgentJob: custom agent values passed through" {
    cd `chart_dir`
    local args=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.agent.defaults.interceptorPort=22101' \
        --set 'ai.agent.defaults.mcpPort=15201' \
        --set 'ai.agent.defaults.hitl.port=16201' \
        --set 'ai.agent.defaults.hitl.approvalTimeout=120s' \
        . | tee /dev/stderr |
        yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

    local actual=$(echo "$args" | yq 'contains(["-interceptor-port=22101"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-mcp-port=15201"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-hitl-port=16201"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-approval-timeout=120s"])')
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# mesh injection disabled

@test "ai/AgentJob: consul mesh inject annotations set to false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.template.metadata.annotations["consul.hashicorp.com/connect-inject"]' | tee /dev/stderr)
    [ "$actual" = "false" ]
}
