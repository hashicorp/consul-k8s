#!/usr/bin/env bats

load _helpers

target=templates/ai-inference-model-configmap.yaml
base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# rendering gate

@test "ai/InferenceConfigMap: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/InferenceConfigMap: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/InferenceConfigMap: rendered when ai.enabled=true and connectInject.enabled=true" {
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

@test "ai/InferenceConfigMap: default interceptorPort in config.json" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.data["config.json"]' | tee /dev/stderr | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['interceptorPort'])")
    [ "$actual" = "21101" ]
}

@test "ai/InferenceConfigMap: default inferenceProtocol in config.json" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.data["config.json"]' | tee /dev/stderr | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['inferenceProtocol'])")
    [ "$actual" = "openai" ]
}

@test "ai/InferenceConfigMap: default inferencePath in config.json" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.data["config.json"]' | tee /dev/stderr | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['inferencePath'])")
    [ "$actual" = "/v1" ]
}

@test "ai/InferenceConfigMap: custom values reflected in config.json" {
    cd `chart_dir`
    local json=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.inferenceModel.defaults.interceptorPort=22000' \
        --set 'ai.inferenceModel.defaults.inferenceProtocol=anthropic' \
        --set 'ai.inferenceModel.defaults.inferencePath=/v2' \
        . | tee /dev/stderr |
        yq '.data["config.json"]' | tee /dev/stderr)

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['interceptorPort'])")
    [ "$actual" = "22000" ]

    local actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['inferenceProtocol'])")
    [ "$actual" = "anthropic" ]
}
