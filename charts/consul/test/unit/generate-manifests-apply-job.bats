#!/usr/bin/env bats

load _helpers

target=templates/generate-manifests-apply-job.yaml

@test "generateManifestsApply/Job: disabled when connectInject.enabled=false" {
  cd `chart_dir`

  local actual=$(helm template \
      --set 'connectInject.enabled=false' \
      --set 'global.generateManifests=false' \
      -s $target \
      . | tee /dev/stderr |
      yq -r '.kind' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "generateManifestsApply/Job: status 0 when enabled" {
  cd `chart_dir`

  run helm template \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      -s $target \
      .

  [ "$status" -eq 0 ]
}

@test "generateManifestsApply/Job: renders Job kind correctly" {
  cd `chart_dir`

  local actual=$(helm template \
      -s $target \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.kind' | tee /dev/stderr)

  [ "${actual}" = "Job" ]
}

@test "generateManifestsApply/Job: contains post-upgrade hook annotation" {
  cd `chart_dir`

  local actual=$(helm template \
      -s $target \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations."helm.sh/hook"' | tee /dev/stderr)

  [ "${actual}" = "post-upgrade" ]
}

@test "generateManifestsApply/Job: contains hook delete policy" {
  cd `chart_dir`

  local actual=$(helm template \
      -s $target \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations."helm.sh/hook-delete-policy"' | tee /dev/stderr)

  [ "${actual}" = "hook-succeeded,before-hook-creation" ]
}

@test "generateManifestsApply/Job: restartPolicy is Never" {
  cd `chart_dir`

  local actual=$(helm template \
      -s $target \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.restartPolicy' | tee /dev/stderr)

  [ "${actual}" = "Never" ]
}

@test "generateManifestsApply/Job: uses default imageApplyManifests (non-OpenShift)" {
  cd `chart_dir`

  local actual=$(helm template \
      -s $target \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)

  [ "${actual}" = "bitnami/kubectl:latest" ]
}

@test "generateManifestsApply/Job: imageApplyManifests override is respected (non-OpenShift)" {
  cd `chart_dir`

  local actual=$(helm template \
      -s $target \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      --set 'global.imageApplyManifests=my-registry/my-kubectl:1.0' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)

  [ "${actual}" = "my-registry/my-kubectl:1.0" ]
}

@test "generateManifestsApply/Job: imageApplyManifests override is respected (OpenShift)" {
  cd `chart_dir`

  local actual=$(helm template \
      -s $target \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      --set 'global.openshift.enabled=true' \
      --set 'global.imageApplyManifests=my-registry/my-oc:1.0' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)

  [ "${actual}" = "my-registry/my-oc:1.0" ]
}
