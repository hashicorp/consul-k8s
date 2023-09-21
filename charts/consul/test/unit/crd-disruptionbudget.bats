#!/usr/bin/env bats

load _helpers

@test "crdGateway/DisruptionBudget: enabled with connectInject=enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-gateways-disruptionbudget.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "crdGateway/DisruptionBudget: disabled with connectInject.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-gateways-disruptionbudget.yaml \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
#--------------------------------------------------------------------
# minAvailable

@test "crdGateway/DisruptionBudget: correct minAvailable" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-gateways-disruptionbudget.yaml \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# apiVersion

@test "crdGateway/DisruptionBudget: uses policy/v1 if supported" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/crd-gateways-disruptionbudget.yaml \
      --api-versions 'policy/v1/PodDisruptionBudget' \
      . | tee /dev/stderr |
      yq -r '.apiVersion' | tee /dev/stderr)
  [ "${actual}" = "policy/v1" ]
}
# NOTE: can't test that it uses policy/v1beta1 if policy/v1 is *not* supported
# because the supported API versions depends on the Helm version and there's
# no flag to *remove* an API version so some Helm versions will always have
# policy/v1 support and will always use that API version.
