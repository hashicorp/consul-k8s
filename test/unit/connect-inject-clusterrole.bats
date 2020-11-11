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

@test "connectInject/ClusterRole: disabled with connectInject.certs.secretName set" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      .
}

@test "connectInject/ClusterRole: enabled with connectInject.certs.secretName not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
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
# global.acls.manageSystemACLs for namespaces

@test "connectInject/ClusterRole: does not allow secret access with global.bootsrapACLs=true and healthChecks.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.healthChecks.enabled=false' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "secrets")) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "connectInject/ClusterRole: secret access with global.bootsrapACLs=true and healthChecks enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "secrets")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: allow secret access with global.bootsrapACLs=true and global.enableConsulNamespaces=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "secrets")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: allows secret access with bootsrapACLs, enablePodSecurityPolicies and enableConsulNamespaces all true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "secrets")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: pod resource permission set when health checks are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.healthChecks.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "pods")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: no pod resource permission set when health checks are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.healthChecks.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "pods")) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "connectInject/ClusterRole: allows secret access with healthChecks and manageSystemACLs, no consulNamespaces" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.healthChecks.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "secrets")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: allows secret access with healthChecks, ACLs and consulNamespaces" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.healthChecks.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "secrets")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: no secret access with healthChecks, and no ACLs" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.healthChecks.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "secrets")) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}
