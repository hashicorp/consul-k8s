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

@test "server/DisruptionBudget: enabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/DisruptionBudget: disabled with server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/DisruptionBudget: disabled with server.disruptionBudget.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.disruptionBudget.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/DisruptionBudget: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# maxUnavailable

@test "server/DisruptionBudget: correct maxUnavailable with replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=1' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=4" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=4' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}


@test "server/DisruptionBudget: correct maxUnavailable with replicas=5" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=5' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=6" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=6' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=7" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=7' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "server/DisruptionBudget: correct maxUnavailable with replicas=8" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-disruptionbudget.yaml  \
      --set 'server.replicas=8' \
      . | tee /dev/stderr |
      yq '.spec.maxUnavailable' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}
