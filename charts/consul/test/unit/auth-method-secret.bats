#!/usr/bin/env bats

load _helpers

@test "auth-method/Secret disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/auth-method-secret.yaml  \
      .
}

@test "auth-method/Secret: enabled with global.acls.manageSystemACLs true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/auth-method-secret.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
