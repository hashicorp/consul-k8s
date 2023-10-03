#!/usr/bin/env bats

load _helpers

@test "ingressGateways/DisruptionBudget: enabled with ingressGateways=enabled , ingressGateways.disruptionBudget.enabled=true " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=true' \
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
  local actual=$(helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.disruptionBudget.enabled=true' \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/DisruptionBudget: disabled with ingressGateways.disruptionBudget.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.disruptionBudget.enabled=false' \
      .
}

#--------------------------------------------------------------------
# minAvailable

@test "ingressGateways/DisruptionBudget: correct minAvailable with replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.replicas=1' \
       --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "ingressGateways/DisruptionBudget: correct minAvailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.replicas=3' \
       --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.minAvailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# maxUnavailable
@test "ingressGateways/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
       -s templates/ingress-gateways-disruptionbudget.yaml \
      --set 'ingressGateways.replicas=1' \
       --set 'ingressGateways.enabled=true' \
      --set 'ingressGateways.disruptionBudget.enabled=true' \
      --set 'ingressGateways.disruptionBudget.maxUnavailable=2' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}
