#!/usr/bin/env bats

load _helpers

@test "controllerLeaderElection/RoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-leader-election-rolebinding.yaml  \
      .
}

@test "controllerLeaderElection/RoleBinding: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-leader-election-rolebinding.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
