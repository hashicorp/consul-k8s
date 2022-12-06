#!/usr/bin/env bats

load _helpers

@test "serviceRouters/CustomResourceDefinition: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-servicerouters.yaml  \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serviceRouters/CustomResourceDefinition: enabled with connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-servicerouters.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      # The generated CRDs have "---" at the top which results in two objects
      # being detected by yq, the first of which is null. We must therefore use
      # yq -s so that length operates on both objects at once rather than
      # individually, which would output false\ntrue and fail the test.
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
