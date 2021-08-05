#!/usr/bin/env bats

load _helpers

@test "client/RoleBinding: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-rolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/RoleBinding: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-rolebinding.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "client/RoleBinding: disabled with client disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-rolebinding.yaml  \
      --set 'client.enabled=false' \
      .
}

@test "client/RoleBinding: enabled with client enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-rolebinding.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/RoleBinding: enabled with client enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-rolebinding.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
