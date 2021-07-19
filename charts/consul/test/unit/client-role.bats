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

@test "client/Role: allows secret access with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

@test "client/Role: allows secret access with global.acls.manageSystemACLs=true and global.enablePodSecurityPolicies=true" {
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

#--------------------------------------------------------------------
# global.openshift.enabled

@test "client/Role: allows securitycontextconstraints access with global.openshift.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[] | select(.resources==["securitycontextconstraints"]) | .resourceNames[0]' | tee /dev/stderr)
  [ "${actual}" = "RELEASE-NAME-consul-client" ]
}

@test "client/Role: allows securitycontextconstraints and acl secret access with global.openshift.enabled=true and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local rules=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[]' | tee /dev/stderr)

  local scc_resource=$(echo $rules | jq -r '. | select(.resources==["securitycontextconstraints"]) | .resourceNames[0]')
  [ "${scc_resource}" = "RELEASE-NAME-consul-client" ]

  local secrets_resource=$(echo $rules | jq -r '. | select(.resources==["secrets"]) | .resourceNames[0]')
  [ "${secrets_resource}" = "RELEASE-NAME-consul-client-acl-token" ]
}

@test "client/Role: allows securitycontextconstraints and psp access with global.openshift.enabled=true and global.enablePodSecurityPolices=true" {
  cd `chart_dir`
  local rules=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[]' | tee /dev/stderr)

  local scc_resource=$(echo $rules | jq -r '. | select(.resources==["securitycontextconstraints"]) | .resourceNames[0]')
  [ "${scc_resource}" = "RELEASE-NAME-consul-client" ]

  local psp_resource=$(echo $rules | jq -r '. | select(.resources==["podsecuritypolicies"]) | .resourceNames[0]')
  [ "${psp_resource}" = "RELEASE-NAME-consul-client" ]
}

@test "client/Role: allows securitycontextconstraints, acl secret, and psp access when all global.openshift.enabled, global.enablePodSecurityPolices, and global.acls.manageSystemACLs are true " {
  cd `chart_dir`
  local rules=$(helm template \
      -s templates/client-role.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[]' | tee /dev/stderr)

  local scc_resource=$(echo $rules | jq -r '. | select(.resources==["securitycontextconstraints"]) | .resourceNames[0]')
  [ "${scc_resource}" = "RELEASE-NAME-consul-client" ]

  local secrets_resource=$(echo $rules | jq -r '. | select(.resources==["secrets"]) | .resourceNames[0]')
  [ "${secrets_resource}" = "RELEASE-NAME-consul-client-acl-token" ]

  local psp_resource=$(echo $rules | jq -r '. | select(.resources==["podsecuritypolicies"]) | .resourceNames[0]')
  [ "${psp_resource}" = "RELEASE-NAME-consul-client" ]
}
