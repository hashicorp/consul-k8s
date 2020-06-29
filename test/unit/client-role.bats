#!/usr/bin/env bats

load _helpers

@test "client/Role: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/Role: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-role.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "client/Role: can be enabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/Role: disabled with client.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=false' \
      .
}

@test "client/Role: enabled with client.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

# The rules key must always be set (#178).
@test "client/Role: rules empty with client.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.rules' | tee /dev/stderr)
  [ "${actual}" = "[]" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "client/Role: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "client/Role: allows secret access with global.bootsrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

@test "client/Role: allows secret access with global.bootsrapACLs=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[1].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}
