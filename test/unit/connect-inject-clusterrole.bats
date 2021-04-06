#!/usr/bin/env bats

load _helpers

@test "connectInject/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      .
}

@test "connectInject/ClusterRole: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/ClusterRole: disabled with connectInject.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=false' \
      .
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "connectInject/ClusterRole: no podsecuritypolicies access with global.enablePodSecurityPolicies=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "connectInject/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "connectInject/ClusterRole: secret access with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "secrets")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
