#!/usr/bin/env bats

load _helpers

target=templates/ai-inference-resources-clusterrole.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/InferenceClusterRole: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/InferenceClusterRole: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/InferenceClusterRole: fails validation when connectInject.enabled=false" {
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

@test "ai/InferenceClusterRole: rendered when ai.enabled=true and connectInject.enabled=true" {
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

@test "ai/InferenceClusterRole: grants get, create, update on inferencemodelconfigs" {
    cd `chart_dir`
    local rules=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.rules[] | select(.resources[] == "inferencemodelconfigs")' | tee /dev/stderr)

    local actual=$(echo "$rules" | yq '.verbs | contains(["get"])' | tee /dev/stderr)
    [ "$actual" = "true" ]

    local actual=$(echo "$rules" | yq '.verbs | contains(["create"])' | tee /dev/stderr)
    [ "$actual" = "true" ]

    local actual=$(echo "$rules" | yq '.verbs | contains(["update"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/InferenceClusterRole: api group is consul.hashicorp.com" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.rules[0].apiGroups[0]' | tee /dev/stderr)
    [ "$actual" = "consul.hashicorp.com" ]
}
