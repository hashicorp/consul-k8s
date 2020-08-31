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

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "controller/Deployment: CONSUL_HTTP_TOKEN env variable created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/Deployment: init container is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/controller-deployment.yaml \
      --set 'controller.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "controller-acl-init" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
