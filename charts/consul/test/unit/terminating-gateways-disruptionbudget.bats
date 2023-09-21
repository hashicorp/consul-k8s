#!/usr/bin/env bats

load _helpers

@test "terminatingGateways/DisruptionBudget: enabled with terminatingGateways=enabled , terminatingGateways.disruptionBudget.enabled=true " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "terminatingGateways/DisruptionBudget: disabled with terminatingGateways.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.enabled=false' \
      .
}

@test "terminatingGateways/DisruptionBudget: disabled with connectInject.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "terminatingGateways/DisruptionBudget: disabled with terminatingGateways.disruptionBudget.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.disruptionBudget.enabled=false' \
      .
}

#--------------------------------------------------------------------
# minAvailable

@test "terminatingGateways/DisruptionBudget: correct minAvailable with replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.replicas=1' \
       --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "terminatingGateways/DisruptionBudget: correct minAvailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.replicas=3' \
       --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# maxUnavailable
@test "terminatingGateways/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.replicas=1' \
       --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.disruptionBudget.enabled=true' \
      --set 'terminatingGateways.disruptionBudget.maxUnavailable=2' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}
