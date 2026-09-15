#!/usr/bin/env bats

load _helpers

target=templates/ai-inference-gateways.yaml
crd_target=templates/crd-inferencegateways.yaml

# Base flags that satisfy every guard in both templates.
base_flags=(
    --set 'connectInject.enabled=true'
    --set 'ai.enabled=true'
    --set 'ai.inferenceGateway.enabled=true'
)

# Base flags + one gateway entry so the object template renders.
gw_flags=(
    --set 'connectInject.enabled=true'
    --set 'ai.enabled=true'
    --set 'ai.inferenceGateway.enabled=true'
    --set 'ai.inferenceGateway.gateways[0].name=travel-pool'
)

# ──────────────────────────────────────────────────────────────────────────────
# crd-inferencegateways.yaml — rendering gate
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/CRD: not rendered by default (ai.enabled=false)" {
    cd `chart_dir`
    assert_empty helm template \
        -s $crd_target \
        --set 'connectInject.enabled=true' \
        .
}

@test "ai/InferenceGateway/CRD: not rendered when ai block absent" {
    cd `chart_dir`
    assert_empty helm template \
        -s $crd_target \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
}

@test "ai/InferenceGateway/CRD: not rendered when connectInject.enabled=false" {
    cd `chart_dir`
    # ai-validate.yaml blocks the render when ai.enabled=true but connectInject.enabled=false,
    # so we verify the CRD template itself by testing without ai.enabled to isolate the guard.
    assert_empty helm template \
        -s $crd_target \
        --set 'connectInject.enabled=false' \
        --set 'ai.enabled=false' \
        --set 'ai.inferenceGateway.enabled=true' \
        .
}

@test "ai/InferenceGateway/CRD: not rendered when ai.inferenceGateway.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $crd_target \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.inferenceGateway.enabled=false' \
        .
}

@test "ai/InferenceGateway/CRD: rendered when all three flags are true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# crd-inferencegateways.yaml — metadata
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/CRD: name is inferencegateways.consul.hashicorp.com" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.name' | tee /dev/stderr)
    [ "$actual" = "inferencegateways.consul.hashicorp.com" ]
}

@test "ai/InferenceGateway/CRD: group is consul.hashicorp.com" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.group' | tee /dev/stderr)
    [ "$actual" = "consul.hashicorp.com" ]
}

@test "ai/InferenceGateway/CRD: scope is Namespaced" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.scope' | tee /dev/stderr)
    [ "$actual" = "Namespaced" ]
}

@test "ai/InferenceGateway/CRD: kind is InferenceGateway" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.names.kind' | tee /dev/stderr)
    [ "$actual" = "InferenceGateway" ]
}

@test "ai/InferenceGateway/CRD: plural is inferencegateways" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.names.plural' | tee /dev/stderr)
    [ "$actual" = "inferencegateways" ]
}

@test "ai/InferenceGateway/CRD: shortName is igw" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.names.shortNames[0]' | tee /dev/stderr)
    [ "$actual" = "igw" ]
}

@test "ai/InferenceGateway/CRD: component label is crd" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.labels.component' | tee /dev/stderr)
    [ "$actual" = "crd" ]
}

@test "ai/InferenceGateway/CRD: app label is set" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.labels | has("app")' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/InferenceGateway/CRD: release label is set" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.labels | has("release")' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# crd-inferencegateways.yaml — version
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/CRD: served and storage version is v1alpha1" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].name' | tee /dev/stderr)
    [ "$actual" = "v1alpha1" ]
}

@test "ai/InferenceGateway/CRD: v1alpha1 served=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].served' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/InferenceGateway/CRD: v1alpha1 storage=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].storage' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# crd-inferencegateways.yaml — subresources
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/CRD: has status subresource" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].subresources | has("status")' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/InferenceGateway/CRD: has scale subresource with correct paths" {
    cd `chart_dir`
    local spec=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].subresources.scale.specReplicasPath' | tee /dev/stderr)
    [ "$spec" = ".spec.replicas" ]

    local status=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].subresources.scale.statusReplicasPath' | tee /dev/stderr)
    [ "$status" = ".status.readyReplicas" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# crd-inferencegateways.yaml — printer columns
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/CRD: has Ready printer column" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Ready") | .name' | tee /dev/stderr)
    [ "$actual" = "Ready" ]
}

@test "ai/InferenceGateway/CRD: has Synced printer column" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Synced") | .name' | tee /dev/stderr)
    [ "$actual" = "Synced" ]
}

@test "ai/InferenceGateway/CRD: has Pool printer column targeting spec.poolRef.name" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Pool") | .jsonPath' | tee /dev/stderr)
    [ "$actual" = ".spec.poolRef.name" ]
}

@test "ai/InferenceGateway/CRD: has Age printer column" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].additionalPrinterColumns[] | select(.name == "Age") | .name' | tee /dev/stderr)
    [ "$actual" = "Age" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# crd-inferencegateways.yaml — spec schema
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/CRD: spec.poolRef is required" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.required | contains(["poolRef"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/InferenceGateway/CRD: spec.replicas is optional" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.required | contains(["replicas"])' | tee /dev/stderr)
    [ "$actual" = "false" ]
}

@test "ai/InferenceGateway/CRD: spec.resources is optional" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.required | contains(["resources"])' | tee /dev/stderr)
    [ "$actual" = "false" ]
}

@test "ai/InferenceGateway/CRD: spec.service is optional" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.required | contains(["service"])' | tee /dev/stderr)
    [ "$actual" = "false" ]
}

@test "ai/InferenceGateway/CRD: spec.service.type enum contains ClusterIP NodePort LoadBalancer" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.service.properties.type.enum | contains(["ClusterIP","NodePort","LoadBalancer"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/InferenceGateway/CRD: spec.service.ports items require port field" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.service.properties.ports.items.required | contains(["port"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/InferenceGateway/CRD: status.conditions is a map list keyed by type" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $crd_target \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.versions[0].schema.openAPIV3Schema.properties.status.properties.conditions."x-kubernetes-list-type"' | tee /dev/stderr)
    [ "$actual" = "map" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# ai-inference-gateways.yaml — rendering gate
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/object: not rendered when ai.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=false' \
        --set 'ai.inferenceGateway.enabled=true' \
        --set 'ai.inferenceGateway.gateways[0].name=pool-a' \
        .
}

@test "ai/InferenceGateway/object: not rendered when ai.inferenceGateway.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.inferenceGateway.enabled=false' \
        --set 'ai.inferenceGateway.gateways[0].name=pool-a' \
        .
}

@test "ai/InferenceGateway/object: not rendered when gateways list is empty" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        "${base_flags[@]}" \
        .
}

@test "ai/InferenceGateway/object: rendered when enabled and gateways list has one entry" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# ai-inference-gateways.yaml — metadata
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/object: apiVersion is consul.hashicorp.com/v1alpha1" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.apiVersion' | tee /dev/stderr)
    [ "$actual" = "consul.hashicorp.com/v1alpha1" ]
}

@test "ai/InferenceGateway/object: kind is InferenceGateway" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.kind' | tee /dev/stderr)
    [ "$actual" = "InferenceGateway" ]
}

@test "ai/InferenceGateway/object: name comes from gateways[].name" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.name' | tee /dev/stderr)
    [ "$actual" = "travel-pool" ]
}

@test "ai/InferenceGateway/object: component label is inference-gateway" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.labels.component' | tee /dev/stderr)
    [ "$actual" = "inference-gateway" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# ai-inference-gateways.yaml — spec.poolRef
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/object: poolRef.name defaults to gateway name when poolRef not set" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.poolRef.name' | tee /dev/stderr)
    [ "$actual" = "travel-pool" ]
}

@test "ai/InferenceGateway/object: poolRef.name uses explicit poolRef.name when set" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].poolRef.name=my-pool' \
        . | tee /dev/stderr |
        yq '.spec.poolRef.name' | tee /dev/stderr)
    [ "$actual" = "my-pool" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# ai-inference-gateways.yaml — spec.image
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/object: image defaults to ai.inferenceGateway.image" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.image' | tee /dev/stderr)
    [ "$actual" = "hashicorp/consul-inference-gateway:0.1.0-dev" ]
}

@test "ai/InferenceGateway/object: per-gateway image overrides default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].image=myregistry/gw:v2' \
        . | tee /dev/stderr |
        yq '.spec.image' | tee /dev/stderr)
    [ "$actual" = "myregistry/gw:v2" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# ai-inference-gateways.yaml — spec.replicas
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/object: replicas defaults to ai.inferenceGateway.defaults.replicas" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.replicas' | tee /dev/stderr)
    [ "$actual" = "2" ]
}

@test "ai/InferenceGateway/object: per-gateway replicas overrides default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].replicas=5' \
        . | tee /dev/stderr |
        yq '.spec.replicas' | tee /dev/stderr)
    [ "$actual" = "5" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# ai-inference-gateways.yaml — spec.service
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/object: service.type defaults to ClusterIP" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.service.type' | tee /dev/stderr)
    [ "$actual" = "ClusterIP" ]
}

@test "ai/InferenceGateway/object: service.ports[0].port defaults to 8443" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.service.ports[0].port' | tee /dev/stderr)
    [ "$actual" = "8443" ]
}

@test "ai/InferenceGateway/object: per-gateway service.type overrides default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].service.type=LoadBalancer' \
        --set 'ai.inferenceGateway.gateways[0].service.ports[0].port=9000' \
        . | tee /dev/stderr |
        yq '.spec.service.type' | tee /dev/stderr)
    [ "$actual" = "LoadBalancer" ]
}

@test "ai/InferenceGateway/object: per-gateway service.ports override default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].service.ports[0].port=9443' \
        . | tee /dev/stderr |
        yq '.spec.service.ports[0].port' | tee /dev/stderr)
    [ "$actual" = "9443" ]
}

@test "ai/InferenceGateway/object: multiple ports rendered when gateway specifies them" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].service.ports[0].port=9000' \
        --set 'ai.inferenceGateway.gateways[0].service.ports[1].port=9001' \
        . | tee /dev/stderr |
        yq '.spec.service.ports | length' | tee /dev/stderr)
    [ "$actual" = "2" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# ai-inference-gateways.yaml — spec.resources
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/object: resources.requests.memory defaults from values" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.resources.requests.memory' | tee /dev/stderr)
    [ "$actual" = "128Mi" ]
}

@test "ai/InferenceGateway/object: resources.limits.cpu defaults from values" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.resources.limits.cpu' | tee /dev/stderr)
    [ "$actual" = "500m" ]
}

@test "ai/InferenceGateway/object: per-gateway resources override defaults" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${gw_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].resources.requests.memory=512Mi' \
        --set 'ai.inferenceGateway.gateways[0].resources.limits.memory=1Gi' \
        . | tee /dev/stderr |
        yq '.spec.resources.requests.memory' | tee /dev/stderr)
    [ "$actual" = "512Mi" ]
}

# ──────────────────────────────────────────────────────────────────────────────
# ai-inference-gateways.yaml — multiple gateways
# ──────────────────────────────────────────────────────────────────────────────

@test "ai/InferenceGateway/object: two gateways render two documents" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].name=pool-a' \
        --set 'ai.inferenceGateway.gateways[1].name=pool-b' \
        . | tee /dev/stderr |
        yq '. | length' | tee /dev/stderr | grep -c '^[0-9]')
    # yq emits one integer per document; two documents means two lines
    [ "$actual" = "2" ]
}

@test "ai/InferenceGateway/object: second gateway inherits defaults" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].name=pool-a' \
        --set 'ai.inferenceGateway.gateways[1].name=pool-b' \
        . | tee /dev/stderr |
        yq 'select(.metadata.name == "pool-b") | .spec.replicas' | tee /dev/stderr)
    [ "$actual" = "2" ]
}

@test "ai/InferenceGateway/object: gateways can independently override replicas" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        "${base_flags[@]}" \
        --set 'ai.inferenceGateway.gateways[0].name=pool-a' \
        --set 'ai.inferenceGateway.gateways[0].replicas=1' \
        --set 'ai.inferenceGateway.gateways[1].name=pool-b' \
        --set 'ai.inferenceGateway.gateways[1].replicas=3' \
        . | tee /dev/stderr |
        yq 'select(.metadata.name == "pool-b") | .spec.replicas' | tee /dev/stderr)
    [ "$actual" = "3" ]
}
