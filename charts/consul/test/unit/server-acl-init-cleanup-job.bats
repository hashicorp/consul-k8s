#!/usr/bin/env bats

load _helpers

@test "serverACLInitCleanup/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      .
}

@test "serverACLInitCleanup/Job: enabled with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInitCleanup/Job: disabled with server=false and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      .
}

@test "serverACLInitCleanup/Job: enabled with client=true and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInitCleanup/Job: disabled when server.updatePartition > 0" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.updatePartition=1' \
      .
}

@test "serverACLInitCleanup/Job: consul-k8s-control-plane delete-completed-job is called with correct arguments" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
       yq -c '.spec.template.spec.containers[0].args' | tee /dev/stderr)
       [ "${actual}" = '["delete-completed-job","-log-level=info","-log-json=false","-k8s-namespace=default","release-name-consul-server-acl-init"]' ]
}

@test "serverACLInitCleanup/Job: enabled with externalServers.enabled=true and global.acls.manageSystemACLs=true, but server.enabled set to false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'server.enabled=false' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.tolerations and global.acls.nodeSelector

@test "serverACLInitCleanup/Job: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "serverACLInitCleanup/Job: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.tolerations=- key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "serverACLInitCleanup/Job: nodeSelector not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "serverACLInitCleanup/Job: nodeSelector can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.nodeSelector=- key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}
