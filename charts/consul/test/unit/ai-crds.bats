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

#--------------------------------------------------------------------
# crd-inferencepoolconfigs.yaml

@test "ai/CRD/InferencePoolConfig: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/CRD/InferencePoolConfig: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/CRD/InferencePoolConfig: not rendered when connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        --set 'ai.enabled=true' \
        --set 'connectInject.enabled=false' \
        .
}

@test "ai/CRD/InferencePoolConfig: rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: name is inferencepoolconfigs.consul.hashicorp.com" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.name' | tee /dev/stderr)
    [ "$actual" = "inferencepoolconfigs.consul.hashicorp.com" ]
}

@test "ai/CRD/InferencePoolConfig: scope is Namespaced" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.scope' | tee /dev/stderr)
    [ "$actual" = "Namespaced" ]
}

@test "ai/CRD/InferencePoolConfig: group is consul.hashicorp.com" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.group' | tee /dev/stderr)
    [ "$actual" = "consul.hashicorp.com" ]
}

@test "ai/CRD/InferencePoolConfig: kind is InferencePoolConfig" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.names.kind' | tee /dev/stderr)
    [ "$actual" = "InferencePoolConfig" ]
}

@test "ai/CRD/InferencePoolConfig: plural is inferencepoolconfigs" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.names.plural' | tee /dev/stderr)
    [ "$actual" = "inferencepoolconfigs" ]
}

@test "ai/CRD/InferencePoolConfig: served and storage version is v1alpha1" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].name' | tee /dev/stderr)
    [ "$actual" = "v1alpha1" ]
}

@test "ai/CRD/InferencePoolConfig: v1alpha1 version is served=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].served' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: v1alpha1 version is storage=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].storage' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: has status subresource" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].subresources | has("status")' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: has Synced printer column" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Synced") | .name' | tee /dev/stderr)
    [ "$actual" = "Synced" ]
}

@test "ai/CRD/InferencePoolConfig: has Age printer column" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Age") | .name' | tee /dev/stderr)
    [ "$actual" = "Age" ]
}

@test "ai/CRD/InferencePoolConfig: Synced column jsonPath targets Ready condition" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Synced") | .jsonPath' | tee /dev/stderr)
    [ "$actual" = '.status.conditions[?(@.type=="Ready")].status' ]
}

@test "ai/CRD/InferencePoolConfig: spec.enabled field is required" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.required | contains(["enabled"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: spec.parentRefs field is required" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.required | contains(["parentRefs"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: spec.enabled has default false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.enabled.default' | tee /dev/stderr)
    [ "$actual" = "false" ]
}

@test "ai/CRD/InferencePoolConfig: spec.parentRefs is an array with minItems=1" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.parentRefs.minItems' | tee /dev/stderr)
    [ "$actual" = "1" ]
}

@test "ai/CRD/InferencePoolConfig: parentRef.kind and parentRef.name are required" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.parentRefs.items.required | contains(["kind","name"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: parentRef.namespace is optional (not in required)" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.parentRefs.items.required | contains(["namespace"])' | tee /dev/stderr)
    [ "$actual" = "false" ]
}

@test "ai/CRD/InferencePoolConfig: spec.rateLimit is optional" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.required | contains(["rateLimit"])' | tee /dev/stderr)
    [ "$actual" = "false" ]
}

@test "ai/CRD/InferencePoolConfig: spec.routing is optional" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.required | contains(["routing"])' | tee /dev/stderr)
    [ "$actual" = "false" ]
}

@test "ai/CRD/InferencePoolConfig: routing.budget has x-kubernetes-preserve-unknown-fields" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.routing.properties.budget."x-kubernetes-preserve-unknown-fields"' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: routing.cache has x-kubernetes-preserve-unknown-fields" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.routing.properties.cache."x-kubernetes-preserve-unknown-fields"' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: routing.mirror has x-kubernetes-preserve-unknown-fields" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.routing.properties.mirror."x-kubernetes-preserve-unknown-fields"' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: status.conditions is a map list keyed by type" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.status.properties.conditions."x-kubernetes-list-type"' | tee /dev/stderr)
    [ "$actual" = "map" ]
}

@test "ai/CRD/InferencePoolConfig: Helm labels contain component=crd" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.labels.component' | tee /dev/stderr)
    [ "$actual" = "crd" ]
}

@test "ai/CRD/InferencePoolConfig: Helm label app is set" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.labels | has("app")' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CRD/InferencePoolConfig: Helm label release is set" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/crd-inferencepoolconfigs.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.labels | has("release")' | tee /dev/stderr)
    [ "$actual" = "true" ]
}
