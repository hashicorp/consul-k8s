#!/usr/bin/env bats

load _helpers

@test "controllerLeaderElection/Role: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-leader-election-role.yaml  \
      .
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
