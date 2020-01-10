#!/usr/bin/env bats

load _helpers

@test "serverACLInit/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-podsecuritypolicy.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/PodSecurityPolicy: disabled with global.bootstrapACLs=true and global.enablePodSecurityPolicies=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-podsecuritypolicy.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/PodSecurityPolicy: enabled with global.bootstrapACLs=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-podsecuritypolicy.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
