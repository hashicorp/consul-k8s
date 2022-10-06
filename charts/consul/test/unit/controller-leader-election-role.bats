#!/usr/bin/env bats

load _helpers

@test "controllerLeaderElection/Role: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-leader-election-role.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controllerLeaderElection/Role: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-leader-election-role.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
