#!/usr/bin/env bats

load _helpers

@test "terminatingGateways/DisruptionBudget: enabled with terminatingGateways=enabled , terminatingGateways.disruptionBudget.enabled=true " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.minAvailable=1' \
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
      --set 'terminatingGateways.defaults.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "terminatingGateways/DisruptionBudget: disabled with terminatingGateways.disruptionBudget.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.defaults.disruptionBudget.enabled=false' \
      .
}

#--------------------------------------------------------------------
# minAvailable

@test "terminatingGateways/DisruptionBudget: correct minAvailable with replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.defaults.replicas=1' \
       --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.minAvailable=1' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "terminatingGateways/DisruptionBudget: min available taking priority over max unavailable" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.replicas=3' \
       --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.disruptionBudget.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.maxUnavailable=3' \
      --set 'terminatingGateways.defaults.disruptionBudget.minAvailable=1' \
      . | tee /dev/stderr)
  [ $(echo "$actual" | yq '.spec.minAvailable') = "1" ]
  [ $(echo "$actual" | yq '.spec.maxUnavailable') = "null" ]
}

@test "terminatingGateways/DisruptionBudget: override minAvailable using child config" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.replicas=1' \
       --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.minAvailable=2' \
      --set 'terminatingGateways.gateways[0].name=unit-test-gateway' \
      --set 'terminatingGateways.gateways[0].disruptionBudget.minAvailable=5' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "5" ]
}

#--------------------------------------------------------------------
# maxUnavailable
@test "terminatingGateways/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.replicas=1' \
       --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.maxUnavailable=2' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "terminatingGateways/DisruptionBudget: override maxUnavailable using child config" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/terminating-gateways-disruptionbudget.yaml \
      --set 'terminatingGateways.replicas=1' \
       --set 'terminatingGateways.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.enabled=true' \
      --set 'terminatingGateways.defaults.disruptionBudget.maxUnavailable=2' \
      --set 'terminatingGateways.gateways[0].name=unit-test-gateway' \
      --set 'terminatingGateways.gateways[0].disruptionBudget.maxUnavailable=5' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "5" ]
}
