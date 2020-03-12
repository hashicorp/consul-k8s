#!/usr/bin/env bats

load _helpers

@test "connectInject/ClusterRole: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ClusterRole: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/ClusterRole: disabled with connectInject.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ClusterRole: disabled with connectInject.certs.secretName set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ClusterRole: enabled with connectInject.certs.secretName not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
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
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      . | tee /dev/stderr |
      yq -r '.rules | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[1].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs for namespaces

@test "connectInject/ClusterRole: does not allow secret access with global.bootsrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: allow secret access with global.bootsrapACLs=true and global.enableConsulNamespaces=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq -r '.rules[1].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

@test "connectInject/ClusterRole: allows secret access with bootsrapACLs, enablePodSecurityPolicies and enableConsulNamespaces all true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq -r '.rules[2].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}
