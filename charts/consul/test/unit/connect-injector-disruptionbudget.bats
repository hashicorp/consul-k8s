#!/usr/bin/env bats

load _helpers

@test "connect-injector/DisruptionBudget: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/connect-injector-disruptionbudget.yaml \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connect-injector/DisruptionBudget: enabled with connectInject=enabled , connectInject.disruptionBudget.enabled=true and global.enabled=true " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connect-injector/DisruptionBudget: disabled with connectInject.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.enabled=false' \
      .
}

@test "connect-injector/DisruptionBudget: disabled with connectInject.disruptionBudget.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.disruptionBudget.enabled=false' \
      .
}

@test "connect-injector/DisruptionBudget: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.enabled=-' \
      --set 'global.enabled=false' \
      .
}

#--------------------------------------------------------------------
# maxUnavailable

@test "connect-injector/DisruptionBudget: correct maxUnavailable with replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=1' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "connect-injector/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=3' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connect-injector/DisruptionBudget: correct maxUnavailable with replicas=4" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=4' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}


@test "connect-injector/DisruptionBudget: correct maxUnavailable with replicas=5" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=5' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connect-injector/DisruptionBudget: correct maxUnavailable with replicas=6" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=6' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "connect-injector/DisruptionBudget: correct maxUnavailable with replicas=7" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=7' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "connect-injector/DisruptionBudget: correct maxUnavailable with replicas=8" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=8' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}

#--------------------------------------------------------------------
# apiVersion

@test "connect-injector/DisruptionBudget: uses policy/v1 if supported" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml \
      --api-versions 'policy/v1/PodDisruptionBudget' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.apiVersion' | tee /dev/stderr)
  [ "${actual}" = "policy/v1" ]
}
# NOTE: can't test that it uses policy/v1beta1 if policy/v1 is *not* supported
# because the supported API versions depends on the Helm version and there's
# no flag to *remove* an API version so some Helm versions will always have
# policy/v1 support and will always use that API version.


#--------------------------------------------------------------------
# minAvailable

@test "connect-injector/DisruptionBudget: correct minAvailable when set" {
  cd `chart_dir`
  local tpl=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=1' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      --set 'connectInject.disruptionBudget.minAvailable=1' \
      . | tee /dev/stderr)
  [ $(echo "$tpl" | yq '.spec.minAvailable') = "1" ]
  [ $(echo "$tpl" | yq '.spec.maxUnavailable') = "null" ]
}

@test "connect-injector/DisruptionBudget: correct minAvailable when set with maxUnavailable" {
  cd `chart_dir`
  local tpl=$(helm template \
      -s templates/connect-injector-disruptionbudget.yaml  \
      --set 'connectInject.replicas=1' \
      --set 'global.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.disruptionBudget.enabled=true' \
      --set 'connectInject.disruptionBudget.minAvailable=1' \
      --set 'connectInject.disruptionBudget.maxUnavailable=2' \
      . | tee /dev/stderr)
  [ $(echo "$tpl" | yq '.spec.minAvailable') = "1" ]
  [ $(echo "$tpl" | yq '.spec.maxUnavailable') = "null" ]
}
