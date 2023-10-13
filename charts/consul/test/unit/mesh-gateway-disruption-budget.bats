#!/usr/bin/env bats

load _helpers

@test "meshGateway/DisruptionBudget: enabled with meshGateway=enabled , meshGateway.disruptionBudget.enabled=true " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-disruptionbudget.yaml \
      --set 'meshGateway.enabled=true' \
      --set 'meshGateway.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/DisruptionBudget: disabled with meshGateway.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/mesh-gateway-disruptionbudget.yaml  \
      --set 'meshGateway.enabled=false' \
      .
}

@test "meshGateway/DisruptionBudget: disabled with connectInject.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-disruptionbudget.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'meshGateway.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/DisruptionBudget: disabled with meshGateway.disruptionBudget.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/mesh-gateway--disruptionbudget.yaml  \
      --set 'meshGateway.disruptionBudget.enabled=false' \
      .
}

#--------------------------------------------------------------------
# minAvailable

@test "meshGateway/DisruptionBudget: minAvailable taking precedence over maxUnavailable" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-disruptionbudget.yaml  \
      --set 'meshGateway.replicas=1' \
       --set 'meshGateway.enabled=true' \
      --set 'meshGateway.disruptionBudget.enabled=true' \
      --set 'meshGateway.disruptionBudget.maxUnavailable=2' \
      --set 'meshGateway.disruptionBudget.minAvailable=1' \
      . | tee /dev/stderr)
  [ $(echo "$actual" | yq '.spec.minAvailable') = "1" ]
  [ $(echo "$actual" | yq '.spec.maxUnavailable') = "null" ]
}

@test "meshGateway/DisruptionBudget: correct minAvailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-disruptionbudget.yaml  \
      --set 'meshGateway.replicas=3' \
       --set 'meshGateway.enabled=true' \
      --set 'meshGateway.disruptionBudget.enabled=true' \
      --set 'meshGateway.disruptionBudget.maxUnavailable=2' \
      --set 'meshGateway.disruptionBudget.minAvailable=1' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# maxUnavailable
@test "meshGateway/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/mesh-gateway-disruptionbudget.yaml  \
      --set 'meshGateway.replicas=1' \
       --set 'meshGateway.enabled=true' \
      --set 'meshGateway.disruptionBudget.enabled=true' \
      --set 'meshGateway.disruptionBudget.maxUnavailable=2' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

#--------------------------------------------------------------------
# apiVersion

@test "meshGateway/DisruptionBudget: uses policy/v1 if supported" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-disruptionbudget.yaml  \
      --set 'meshGateway.replicas=1' \
       --set 'meshGateway.enabled=true' \
      --set 'meshGateway.disruptionBudget.enabled=true' \
      --set 'meshGateway.disruptionBudget.maxUnavailable=2' \
      --api-versions 'policy/v1/PodDisruptionBudget' \
      . | tee /dev/stderr |
      yq -r '.apiVersion' | tee /dev/stderr)
  [ "${actual}" = "policy/v1" ]
}
# NOTE: can't test that it uses policy/v1beta1 if policy/v1 is *not* supported
# because the supported API versions depends on the Helm version and there's
# no flag to *remove* an API version so some Helm versions will always have
# policy/v1 support and will always use that API version.
