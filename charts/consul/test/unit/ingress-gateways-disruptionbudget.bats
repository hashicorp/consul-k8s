#!/usr/bin/env bats

load _helpers

@test "ingressGateways/DisruptionBudget: enabled with ingressGateways=enabled , ingressGateways.disruptionBudget.enabled=true " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.minAvailable=1' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/DisruptionBudget: disabled with ingressGateways.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.enabled=false' \
      .
}

@test "ingressGateways/DisruptionBudget: disabled with connectInject.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=false' \
      . [ "$status" -eq 1 ]
        [[ "$output" =~ "connectInject.enabled must be true" ]]
}

@test "ingressGateways/DisruptionBudget: disabled with ingressGateways.disruptionBudget.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.defaults.disruptionBudget.enabled=false' \
      .
}

#--------------------------------------------------------------------
# minAvailable

@test "ingressGateways/DisruptionBudget: correct minAvailable with replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.defaults.replicas=1' \
       --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.minAvailable=1' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "ingressGateways/DisruptionBudget: min available taking priority over max unavailable" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.replicas=3' \
       --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.maxUnavailable=3' \
      --set 'ingressGateways.defaults.disruptionBudget.minAvailable=1' \
      . | tee /dev/stderr)
  [ $(echo "$actual" | yq '.spec.minAvailable') = "1" ]
  [ $(echo "$actual" | yq '.spec.maxUnavailable') = "null" ]
}

@test "ingressGateways/DisruptionBudget: override minAvailable using child config" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.replicas=1' \
       --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.minAvailable=2' \
      --set 'ingressGateways.gateways[0].name=unit-test-gateway' \
      --set 'ingressGateways.gateways[0].disruptionBudget.minAvailable=5' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "5" ]
}

#--------------------------------------------------------------------
# maxUnavailable
@test "ingressGateways/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.replicas=1' \
       --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.maxUnavailable=2' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "ingressGateways/DisruptionBudget: override maxUnavailable using child config" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.replicas=1' \
       --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.enabled=true' \
      --set 'ingressGateways.defaults.disruptionBudget.maxUnavailable=2' \
      --set 'ingressGateways.gateways[0].name=unit-test-gateway' \
      --set 'ingressGateways.gateways[0].disruptionBudget.maxUnavailable=5' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "5" ]
}
