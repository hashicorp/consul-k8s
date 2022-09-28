#!/usr/bin/env bats

load _helpers

@test "ingressGateway/CustomResourceDefinition: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-ingressgateways.yaml  \
      . | tee /dev/stderr |
      # The generated CRDs have "---" at the top which results in two objects
      # being detected by yq, the first of which is null. We must therefore use
      # yq -s so that length operates on both objects at once rather than
      # individually, which would output false\ntrue and fail the test.
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateway/CustomResourceDefinition: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-ingressgateways.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      # The generated CRDs have "---" at the top which results in two objects
      # being detected by yq, the first of which is null. We must therefore use
      # yq -s so that length operates on both objects at once rather than
      # individually, which would output false\ntrue and fail the test.
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateway/CustomResourceDefinition: disabled with controller.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/crd-meshes.yaml  \
      --set 'controller.enabled=false' \
      .
}
