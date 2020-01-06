#!/usr/bin/env bats

load _helpers

@test "serverACLInitCleanup/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-podsecuritypolicy.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInitCleanup/PodSecurityPolicy: disabled with global.bootstrapACLs=true and global.enablePodSecurityPolicies=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-podsecuritypolicy.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInitCleanup/PodSecurityPolicy: enabled with global.bootstrapACLs=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-podsecuritypolicy.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
