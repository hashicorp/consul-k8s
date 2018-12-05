#!/usr/bin/env bats

load _helpers

@test "server/DisruptionBudget: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/DisruptionBudget: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/DisruptionBudget: disable with server.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/DisruptionBudget: disable with server.disruptionBudget.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.disruptionBudget.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/DisruptionBudget: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with n=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with n=4" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=4' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}


@test "server/DisruptionBudget: correct maxUnavailable with n=5" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=5' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with n=6" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=6' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with n=7" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=7' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with n=8" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=8' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}