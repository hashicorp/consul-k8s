#!/usr/bin/env bats

load _helpers

@test "mesh/CustomResourceDefinition: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-meshes.yaml  \
      . | tee /dev/stderr |
      # The generated CRDs have "---" at the top which results in two objects
      # being detected by yq, the first of which is null. We must therefore use
      # yq -s so that length operates on both objects at once rather than
      # individually, which would output false\ntrue and fail the test.
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "mesh/CustomResourceDefinition: enabled with connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-meshes.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      # The generated CRDs have "---" at the top which results in two objects
      # being detected by yq, the first of which is null. We must therefore use
      # yq -s so that length operates on both objects at once rather than
      # individually, which would output false\ntrue and fail the test.
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateway/CustomResourceDefinition: disabled with connectInject.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/crd-meshes.yaml  \
      --set 'connectInject.enabled=false' \
      .
}
