#!/usr/bin/env bats

load _helpers

@test "serverACLInit/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-acl-init-podsecuritypolicy.yaml  \
      .
}

@test "serverACLInit/PodSecurityPolicy: disabled with global.acls.manageSystemACLs=true and global.enablePodSecurityPolicies=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-acl-init-podsecuritypolicy.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      .
}

@test "serverACLInit/PodSecurityPolicy: enabled with global.acls.manageSystemACLs=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-podsecuritypolicy.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/PodSecurityPolicy: enabled with externalServers.enabled=true and global.acls.manageSystemACLs=true, but server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-podsecuritypolicy.yaml  \
      --set 'server.enabled=false' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
