#!/usr/bin/env bats

load _helpers

base_flags=(--set 'connectInject.enabled=true' --set 'ai.enabled=true')

#--------------------------------------------------------------------
# crd-inferencemodelconfigs.yaml

@test "ai/CRD/InferenceModelConfig: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-inferencemodelconfigs.yaml \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/CRD/InferenceModelConfig: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-inferencemodelconfigs.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/CRD/InferenceModelConfig: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencemodelconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferenceModelConfig: name is inferencemodelconfigs.consul.hashicorp.com" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencemodelconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.name' | tee /dev/stderr)
    [ "$actual" = "inferencemodelconfigs.consul.hashicorp.com" ]
}

@test "ai/CRD/InferenceModelConfig: has Synced printer column" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencemodelconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Synced") | .name' | tee /dev/stderr)
    [ "$actual" = "Synced" ]
}

@test "ai/CRD/InferenceModelConfig: scope is Cluster" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencemodelconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.scope' | tee /dev/stderr)
    [ "$actual" = "Cluster" ]
}

#--------------------------------------------------------------------
# crd-mcpserverconfigs.yaml

@test "ai/CRD/McpServerConfig: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-mcpserverconfigs.yaml \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/CRD/McpServerConfig: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-mcpserverconfigs.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/CRD/McpServerConfig: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-mcpserverconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/McpServerConfig: name is mcpserverconfigs.consul.hashicorp.com" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-mcpserverconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.name' | tee /dev/stderr)
    [ "$actual" = "mcpserverconfigs.consul.hashicorp.com" ]
}

@test "ai/CRD/McpServerConfig: has Synced printer column" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-mcpserverconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Synced") | .name' | tee /dev/stderr)
    [ "$actual" = "Synced" ]
}

#--------------------------------------------------------------------
# crd-agentconfigs.yaml

@test "ai/CRD/AgentConfig: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-agentconfigs.yaml \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/CRD/AgentConfig: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-agentconfigs.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/CRD/AgentConfig: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-agentconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/AgentConfig: name is agentconfigs.consul.hashicorp.com" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-agentconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.name' | tee /dev/stderr)
    [ "$actual" = "agentconfigs.consul.hashicorp.com" ]
}

@test "ai/CRD/AgentConfig: has Synced printer column" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-agentconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Synced") | .name' | tee /dev/stderr)
    [ "$actual" = "Synced" ]
}

@test "ai/CRD/AgentConfig: scope is Cluster" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-agentconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.scope' | tee /dev/stderr)
    [ "$actual" = "Cluster" ]
}
