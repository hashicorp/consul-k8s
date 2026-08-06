#!/usr/bin/env bats

load _helpers

target=templates/ai-agent-configmap.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/AgentConfigMap: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/AgentConfigMap: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/AgentConfigMap: rendered when ai.enabled=true and connectInject.enabled=true" {
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

@test "ai/AgentConfigMap: default values in config.json" {
    cd `chart_dir`
    local json=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.data["config.json"]' | tee /dev/stderr)

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['interceptorPort'])")
    [ "$actual" = "21101" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['mcpPort'])")
    [ "$actual" = "15101" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['hitl']['port'])")
    [ "$actual" = "16101" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['hitl']['approvalTimeout'])")
    [ "$actual" = "60s" ]
}

@test "ai/AgentConfigMap: custom values reflected in config.json" {
    cd `chart_dir`
    local json=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.agent.defaults.interceptorPort=22101' \
        --set 'ai.agent.defaults.mcpPort=15201' \
        --set 'ai.agent.defaults.hitl.port=16201' \
        --set 'ai.agent.defaults.hitl.approvalTimeout=120s' \
        . | tee /dev/stderr |
        yq '.data["config.json"]' | tee /dev/stderr)

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['interceptorPort'])")
    [ "$actual" = "22101" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['hitl']['approvalTimeout'])")
    [ "$actual" = "120s" ]
}
