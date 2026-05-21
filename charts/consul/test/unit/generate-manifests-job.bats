#!/usr/bin/env bats

load _helpers

@test "generateManifests/Job: disabled when connectInject.enabled=false" {
  cd `chart_dir`

  run helm template \
      --set 'connectInject.enabled=false' \
      --set 'global.generateManifests=false' \
      -s templates/generate-manifests-job.yaml \
      .

  [ "$status" -ne 0 ]
}

@test "generateManifests/Job: status 0 when enabled" {
  cd `chart_dir`

  run helm template \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      -s templates/generate-manifests-job.yaml \
      .

  [ "$status" -eq 0 ]
}

@test "generateManifests/Job: enabled when connectInject.enabled and global.generateManifests are true" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.kind' | tee /dev/stderr)

  [ "${actual}" = "Job" ]
}

@test "generateManifests/Job: renders Job kind correctly" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.kind' | tee /dev/stderr)

  [ "${actual}" = "Job" ]
}

@test "generateManifests/Job: contains pre-upgrade hook annotation" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations."helm.sh/hook"' | tee /dev/stderr)

  [ "${actual}" = "pre-upgrade" ]
}

@test "generateManifests/Job: contains hook delete policy" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations."helm.sh/hook-delete-policy"' | tee /dev/stderr)

  [ "${actual}" = "hook-succeeded,before-hook-creation" ]
}

@test "generateManifests/Job: restartPolicy is Never" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.restartPolicy' | tee /dev/stderr)

  [ "${actual}" = "Never" ]
}

@test "generateManifests/Job: mounts gatewayapi pvc correctly" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[0].persistentVolumeClaim.claimName' | tee /dev/stderr)

  [ "${actual}" = "release-name-consul-gatewayapi-pvc" ]
}

@test "generateManifests/Job: mounts output directory correctly" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].volumeMounts[0].mountPath' | tee /dev/stderr)

  [ "${actual}" = "/output/" ]
}

@test "generateManifests/Job: consulapi-enabled argument added when consulapi CRD enabled" {
  cd `chart_dir`

  run bash -c "
    helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      --set 'global.crds.consulapi.enabled=true' \
      . | yq -r '.spec.template.spec.containers[0].args[]' | grep '^-consulapi-enabled=true$'
  "

  [ "$status" -eq 0 ]
}

@test "generateManifests/Job: openshift scc argument added when openshift enabled" {
  cd `chart_dir`

  run bash -c "
    helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      --set 'global.openshift.enabled=true' \
      --set 'connectInject.apiGateway.managedGatewayClass.openshiftSCCName=restricted-v2' \
      . | yq -r '.spec.template.spec.containers[0].args[]' | grep '^-openshift-scc-name=restricted-v2$'
  "

  [ "$status" -eq 0 ]
}

@test "generateManifests/Job: renders configured image" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      --set 'global.imageK8S=hashicorp/consul-k8s-control-plane:2.0.0' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)

  [ "${actual}" = "hashicorp/consul-k8s-control-plane:2.0.0" ]
}

@test "generateManifests/Job: renders podSecurityContext when enabled" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/generate-manifests-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.generateManifests=true' \
      --set 'global.podSecurityContext.enabled=true' \
      --set 'global.openshift.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext.fsGroup' | tee /dev/stderr)

  [ "${actual}" = "100" ]
}