#!/usr/bin/env bats

load _helpers

@test "createFederationSecret/Role: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-role.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "createFederationSecret/Role: enabled with global.federation.createFederationSecret=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-role.yaml  \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "createFederationSecret/Role: allows read access for replication token with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-role.yaml  \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.createReplicationToken=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resourceNames[0] == "release-name-consul-acl-replication-acl-token")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "createFederationSecret/Role: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/create-federation-secret-role.yaml  \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
