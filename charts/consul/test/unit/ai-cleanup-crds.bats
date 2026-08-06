#!/usr/bin/env bats

load _helpers

base_flags=(--set 'connectInject.enabled=false' --set 'global.enabled=false')

#--------------------------------------------------------------------
# rendering gate — cleanup renders when ai.enabled=false (or absent)

@test "ai/CleanupJob: rendered when ai.enabled=false (cleanup needed)" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupJob: rendered when ai block absent" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        --set 'ai=null' \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupJob: not rendered when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        .
}

#--------------------------------------------------------------------
# hook annotations

@test "ai/CleanupJob: has pre-delete,pre-upgrade hook annotation" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.annotations["helm.sh/hook"]' | tee /dev/stderr)
    [ "$actual" = "pre-delete,pre-upgrade" ]
}

@test "ai/CleanupJob: hook-weight is -5" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.annotations["helm.sh/hook-weight"]' | tee /dev/stderr)
    [ "$actual" = "-5" ]
}

@test "ai/CleanupJob: has hook-delete-policy annotation" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.annotations["helm.sh/hook-delete-policy"]' | tee /dev/stderr)
    [ "$actual" = "hook-succeeded,before-hook-creation" ]
}

#--------------------------------------------------------------------
# subcommand

@test "ai/CleanupJob: runs ai-cleanup subcommand" {
    cd `chart_dir`
    local args=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

    local actual=$(echo "$args" | yq 'contains(["ai-cleanup"])')
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# cleanup ClusterRole

@test "ai/CleanupClusterRole: rendered when ai.enabled=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupClusterRole: not rendered when ai.enabled=true" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        .
}

@test "ai/CleanupClusterRole: has hook-weight -10" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.metadata.annotations["helm.sh/hook-weight"]' | tee /dev/stderr)
    [ "$actual" = "-10" ]
}

@test "ai/CleanupClusterRole: includes all three AI CRD resources" {
    cd `chart_dir`
    local rules=$(helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.rules[0].resources' | tee /dev/stderr)

    local actual=$(echo "$rules" | yq 'contains(["inferencemodelconfigs"])' | tee /dev/stderr)
    [ "$actual" = "true" ]

    local actual=$(echo "$rules" | yq 'contains(["mcpserverconfigs"])' | tee /dev/stderr)
    [ "$actual" = "true" ]

    local actual=$(echo "$rules" | yq 'contains(["agentconfigs"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupClusterRole: includes delete verb" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq '.rules[0].verbs | contains(["delete"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# cleanup ClusterRoleBinding

@test "ai/CleanupClusterRoleBinding: rendered when ai.enabled=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-clusterrolebinding.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupClusterRoleBinding: not rendered when ai.enabled=true" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/ai-cleanup-crds-clusterrolebinding.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        .
}

#--------------------------------------------------------------------
# cleanup ServiceAccount

@test "ai/CleanupServiceAccount: rendered when ai.enabled=false" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-serviceaccount.yaml \
        "${base_flags[@]}" \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupServiceAccount: not rendered when ai.enabled=true" {
    cd `chart_dir`
    assert_empty helm template \
        -s templates/ai-cleanup-crds-serviceaccount.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        .
}

@test "ai/CleanupServiceAccount: can set imagePullSecrets" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-serviceaccount.yaml \
        "${base_flags[@]}" \
        --set 'global.imagePullSecrets[0].name=pull-secret' \
        . | tee /dev/stderr |
        yq '.imagePullSecrets[0].name' | tee /dev/stderr)
    [ "$actual" = "pull-secret" ]
}
