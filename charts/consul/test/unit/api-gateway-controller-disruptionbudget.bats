#!/usr/bin/env bats

load _helpers

@test "apiGateway/DisruptionBudget: enabled with apiGateway=enabled , apiGateway.disruptionBudget.enabled=true " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-disruptionbudget.yaml \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.disruptionBudget.enabled=true' \
      --set 'apiGateway.image=something' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/DisruptionBudget: disabled with apiGateway.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-disruptionbudget.yaml  \
      --set 'apiGateway.enabled=false' \
      .
}

@test "apiGateway/DisruptionBudget: disabled with apiGateway.disruptionBudget.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-disruptionbudget.yaml  \
      --set 'apiGateway.disruptionBudget.enabled=false' \
      .
}

#--------------------------------------------------------------------
# minAvailable

@test "apiGateway/DisruptionBudget: correct minAvailable with replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-disruptionbudget.yaml  \
      --set 'apiGateway.replicas=1' \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.disruptionBudget.enabled=true' \
      --set 'apiGateway.image=something' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "apiGateway/DisruptionBudget: correct minAvailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-disruptionbudget.yaml  \
      --set 'apiGateway.replicas=3' \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.disruptionBudget.enabled=true' \
      --set 'apiGateway.image=something' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# maxUnavailable
@test "apiGateway/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-disruptionbudget.yaml  \
      --set 'apiGateway.replicas=3' \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.disruptionBudget.enabled=true' \
      --set 'apiGateway.image=something' \
      --set 'apiGateway.disruptionBudget.maxUnavailable=2' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

#--------------------------------------------------------------------
# apiVersion

@test "apiGateway/DisruptionBudget: uses policy/v1 if supported" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-disruptionbudget.yaml  \
      --set 'apiGateway.replicas=3' \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.disruptionBudget.enabled=true' \
      --set 'apiGateway.image=something' \
      --api-versions 'policy/v1/PodDisruptionBudget' \
      . | tee /dev/stderr |
      yq -r '.apiVersion' | tee /dev/stderr)
  [ "${actual}" = "policy/v1" ]
}
# NOTE: can't test that it uses policy/v1beta1 if policy/v1 is *not* supported
# because the supported API versions depends on the Helm version and there's
# no flag to *remove* an API version so some Helm versions will always have
# policy/v1 support and will always use that API version.
