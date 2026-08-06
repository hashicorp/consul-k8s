#!/usr/bin/env bats

load _helpers

target=templates/ai-inference-resources-job.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/InferenceJob: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/InferenceJob: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/InferenceJob: fails validation when connectInject.enabled=false" {
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

@test "ai/InferenceJob: rendered when ai.enabled=true and connectInject.enabled=true" {
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

@test "ai/InferenceJob: has post-install,post-upgrade hook annotation" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.annotations["helm.sh/hook"]' | tee /dev/stderr)
    [ "$actual" = "post-install,post-upgrade" ]
}

@test "ai/InferenceJob: has hook-delete-policy annotation" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.annotations["helm.sh/hook-delete-policy"]' | tee /dev/stderr)
    [ "$actual" = "hook-succeeded,before-hook-creation" ]
}

#--------------------------------------------------------------------
# image

@test "ai/InferenceJob: uses global.imageK8S" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'global.imageK8S=my-image:1.2.3' \
        . | tee /dev/stderr |
        yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
    [ "$actual" = "my-image:1.2.3" ]
}

#--------------------------------------------------------------------
# args

@test "ai/InferenceJob: default args passed to subcommand" {
    cd `chart_dir`
    local args=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

    local actual=$(echo "$args" | yq 'contains(["ai-inference-resources"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-enabled=true"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-interceptor-port=21101"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-inference-path=/v1"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-inference-protocol=openai"])')
    [ "$actual" = "true" ]
}

@test "ai/InferenceJob: custom inferenceModel values passed through" {
    cd `chart_dir`
    local args=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.inferenceModel.defaults.interceptorPort=22000' \
        --set 'ai.inferenceModel.defaults.inferenceProtocol=anthropic' \
        --set 'ai.inferenceModel.defaults.inferencePath=/v2' \
        . | tee /dev/stderr |
        yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

    local actual=$(echo "$args" | yq 'contains(["-interceptor-port=22000"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-inference-protocol=anthropic"])')
    [ "$actual" = "true" ]

    local actual=$(echo "$args" | yq 'contains(["-inference-path=/v2"])')
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# mesh injection disabled

@test "ai/InferenceJob: consul mesh inject annotations set to false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.template.metadata.annotations["consul.hashicorp.com/connect-inject"]' | tee /dev/stderr)
    [ "$actual" = "false" ]
}
