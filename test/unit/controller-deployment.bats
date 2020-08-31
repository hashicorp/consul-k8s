#!/usr/bin/env bats

load _helpers

@test "controller/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-deployment.yaml  \
      .
}

@test "controller/Deployment: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# replicas

@test "controller/Deployment: replicas defaults to 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "controller/Deployment: can set replicas" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml  \
      --set 'controller.enabled=true' \
      --set 'controller.replicas=2' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}
