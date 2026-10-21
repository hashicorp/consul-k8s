#!/usr/bin/env bats

load _helpers

base_flags=(--set 'connectInject.enabled=false' --set 'global.enabled=false' --set 'ai.enabled=false')
cleanup_templates=(
    templates/ai-cleanup-crds-job.yaml
    templates/ai-cleanup-crds-clusterrole.yaml
    templates/ai-cleanup-crds-clusterrolebinding.yaml
    templates/ai-cleanup-crds-serviceaccount.yaml
)

assert_cleanup_hooks() {
    cd `chart_dir`
    local lifecycle="$1" setting="$2" expected="$3" template manifest
    local lifecycle_flags=()
    if [ "$lifecycle" = "upgrade" ]; then
        lifecycle_flags=(--is-upgrade)
    fi
    for template in "${cleanup_templates[@]}"; do
        run helm template \
            -s "$template" \
            "${base_flags[@]}" \
            "${lifecycle_flags[@]}" \
            --set 'connectInject.enabled=true' \
            --set "$setting" \
            .
        echo "$template ($lifecycle, $setting): $output"
        [ "$status" -eq 0 ]
        manifest="$output"
        run yq -r '.metadata.annotations["helm.sh/hook"]' <<< "$manifest"
        [ "$status" -eq 0 ]
        [ "$output" = "$expected" ]
    done
}

#--------------------------------------------------------------------
# Always render all cleanup resources; hooks never execute on initial install.

@test "ai/Cleanup: install with ai.enabled=true renders only pre-delete hooks" {
    assert_cleanup_hooks install ai.enabled=true pre-delete
}

@test "ai/Cleanup: install with ai.enabled=false renders pre-delete,pre-upgrade hooks" {
    assert_cleanup_hooks install ai.enabled=false pre-delete,pre-upgrade
}

@test "ai/Cleanup: install with ai.enabled=null renders pre-delete,pre-upgrade hooks" {
    assert_cleanup_hooks install ai.enabled=null pre-delete,pre-upgrade
}

@test "ai/Cleanup: install with AI absent renders pre-delete,pre-upgrade hooks" {
    assert_cleanup_hooks install ai=null pre-delete,pre-upgrade
}

@test "ai/Cleanup: upgrade with ai.enabled=true renders only pre-delete hooks" {
    assert_cleanup_hooks upgrade ai.enabled=true pre-delete
}

@test "ai/Cleanup: upgrade with ai.enabled=false renders pre-delete,pre-upgrade hooks" {
    assert_cleanup_hooks upgrade ai.enabled=false pre-delete,pre-upgrade
}

@test "ai/Cleanup: upgrade with ai.enabled=null renders pre-delete,pre-upgrade hooks" {
    assert_cleanup_hooks upgrade ai.enabled=null pre-delete,pre-upgrade
}

@test "ai/Cleanup: upgrade with AI absent renders pre-delete,pre-upgrade hooks" {
    assert_cleanup_hooks upgrade ai=null pre-delete,pre-upgrade
}

#--------------------------------------------------------------------
# hook annotations

@test "ai/CleanupJob: has pre-delete,pre-upgrade hook annotation" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        . | tee /dev/stderr |
        yq -r '.metadata.annotations["helm.sh/hook"]' | tee /dev/stderr)
    [ "$actual" = "pre-delete,pre-upgrade" ]
}

@test "ai/CleanupJob: hook-weight is -5" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        . | tee /dev/stderr |
        yq -r '.metadata.annotations["helm.sh/hook-weight"]' | tee /dev/stderr)
    [ "$actual" = "-5" ]
}

@test "ai/CleanupJob: has hook-delete-policy annotation" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        . | tee /dev/stderr |
        yq -r '.metadata.annotations["helm.sh/hook-delete-policy"]' | tee /dev/stderr)
    [ "$actual" = "hook-succeeded,before-hook-creation,hook-failed" ]
}

#--------------------------------------------------------------------
# subcommand

@test "ai/CleanupJob: runs ai-cleanup subcommand" {
    cd `chart_dir`
    local args=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        . | tee /dev/stderr |
        yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

    local actual=$(echo "$args" | yq -r 'contains(["ai-cleanup"])')
    [ "$actual" = "true" ]
}

@test "ai/CleanupJob: has a 120-second active deadline" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-job.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        . | tee /dev/stderr |
        yq -r '.spec.activeDeadlineSeconds' | tee /dev/stderr)
    [ "$actual" = "120" ]
}

#--------------------------------------------------------------------
# cleanup ClusterRole

@test "ai/CleanupClusterRole: rendered when ai.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        . | tee /dev/stderr |
        yq -r 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupClusterRole: rendered on initial install when ai.enabled=false" {
    cd `chart_dir`
    run helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        "${base_flags[@]}" \
        .
    [ "$status" -eq 0 ]
}

@test "ai/CleanupClusterRole: has hook-weight -10" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        . | tee /dev/stderr |
        yq -r '.metadata.annotations["helm.sh/hook-weight"]' | tee /dev/stderr)
    [ "$actual" = "-10" ]
}

@test "ai/CleanupClusterRole: includes all three AI CRD resources" {
    cd `chart_dir`
    local rules=$(helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        . | tee /dev/stderr |
        yq -r '.rules[0].resources' | tee /dev/stderr)

    local actual=$(echo "$rules" | yq -r 'contains(["inferencemodelconfigs"])' | tee /dev/stderr)
    [ "$actual" = "true" ]

    local actual=$(echo "$rules" | yq -r 'contains(["mcpserverconfigs"])' | tee /dev/stderr)
    [ "$actual" = "true" ]

    local actual=$(echo "$rules" | yq -r 'contains(["agentconfigs"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupClusterRole: includes delete verb" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-clusterrole.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        . | tee /dev/stderr |
        yq -r '.rules[0].verbs | contains(["delete"])' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

#--------------------------------------------------------------------
# cleanup ClusterRoleBinding

@test "ai/CleanupClusterRoleBinding: rendered when ai.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-clusterrolebinding.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        . | tee /dev/stderr |
        yq -r 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupClusterRoleBinding: rendered on initial install when ai.enabled=false" {
    cd `chart_dir`
    run helm template \
        -s templates/ai-cleanup-crds-clusterrolebinding.yaml \
        "${base_flags[@]}" \
        .
    [ "$status" -eq 0 ]
}

#--------------------------------------------------------------------
# cleanup ServiceAccount

@test "ai/CleanupServiceAccount: rendered when ai.enabled=true" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-serviceaccount.yaml \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        . | tee /dev/stderr |
        yq -r 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "ai/CleanupServiceAccount: rendered on initial install when ai.enabled=false" {
    cd `chart_dir`
    run helm template \
        -s templates/ai-cleanup-crds-serviceaccount.yaml \
        "${base_flags[@]}" \
        .
    [ "$status" -eq 0 ]
}

@test "ai/CleanupServiceAccount: can set imagePullSecrets" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/ai-cleanup-crds-serviceaccount.yaml \
        "${base_flags[@]}" \
        --is-upgrade \
        --set 'global.imagePullSecrets[0].name=pull-secret' \
        . | tee /dev/stderr |
        yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
    [ "$actual" = "pull-secret" ]
}
