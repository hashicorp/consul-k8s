#!/usr/bin/env bats

load _helpers

target=templates/ai-agent-resources-clusterrolebinding.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/AgentClusterRoleBinding: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/AgentClusterRoleBinding: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/AgentClusterRoleBinding: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/AgentClusterRoleBinding: references correct ClusterRole" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.roleRef.kind' | tee /dev/stderr)
    [ "$actual" = "ClusterRole" ]
}

@test "ai/AgentClusterRoleBinding: subject is ServiceAccount" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.subjects[0].kind' | tee /dev/stderr)
    [ "$actual" = "ServiceAccount" ]
}
