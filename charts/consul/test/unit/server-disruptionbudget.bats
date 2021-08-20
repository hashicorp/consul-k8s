#!/usr/bin/env bats

load _helpers

@test "server/DisruptionBudget: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/DisruptionBudget: enabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/DisruptionBudget: disabled with server.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.enabled=false' \
      .
}

@test "server/DisruptionBudget: disabled with server.disruptionBudget.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.disruptionBudget.enabled=false' \
      .
}

@test "server/DisruptionBudget: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'global.enabled=false' \
      .
}

#--------------------------------------------------------------------
# maxUnavailable

@test "server/DisruptionBudget: correct maxUnavailable with replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=1' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=4" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=4' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}


@test "server/DisruptionBudget: correct maxUnavailable with replicas=5" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=5' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=6" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=6' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=7" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=7' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=8" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=8' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}

#--------------------------------------------------------------------
# apiVersion

@test "server/DisruptionBudget: uses policy/v1 if supported" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-disruptionbudget.yaml \
      --api-versions 'policy/v1' \
      . | tee /dev/stderr |
      yq -r '.apiVersion' | tee /dev/stderr)
  [ "${actual}" = "policy/v1" ]
}
# NOTE: can't test that it uses policy/v1beta1 if policy/v1 is *not* supported
# because the supported API versions depends on the Helm version and there's
# no flag to *remove* an API version so some Helm versions will always have
# policy/v1 support and will always use that API version.

