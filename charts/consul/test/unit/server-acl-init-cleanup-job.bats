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

#--------------------------------------------------------------------
# extraLabels

@test "serverACLInitCleanup/Job: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "serverACLInitCleanup/Job: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "serverACLInitCleanup/Job: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.extraLabels.foo=bar' \
      --set 'global.extraLabels.baz=qux' \
      . | tee /dev/stderr)
  local actualFoo=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  local actualBaz=$(echo "${actual}" | yq -r '.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualFoo}" = "bar" ]
  [ "${actualBaz}" = "qux" ]
  local actualTemplateFoo=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  local actualTemplateBaz=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualTemplateFoo}" = "bar" ]
  [ "${actualTemplateBaz}" = "qux" ]
}

#--------------------------------------------------------------------
# logLevel

@test "serverACLInitCleanup/Job: use global.logLevel by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInitCleanup/Job: override global.logLevel when global.acls.logLevel is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.logLevel=debug' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
# resources

@test "serverACLInitCleanup/Job: resources defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"50m","memory":"50Mi"},"requests":{"cpu":"50m","memory":"50Mi"}}' ]
}

@test "serverACLInitCleanup/Job: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.resources.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# server.containerSecurityContext.aclInit

@test "serverACLInitCleanup/Job: securityContext is set when server.containerSecurityContext.aclInit is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.containerSecurityContext.aclInit.runAsUser=100' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext.runAsUser' | tee /dev/stderr)

  [ "${actual}" = "100" ]
}

#--------------------------------------------------------------------
# annotations

@test "serverACLInitCleanup/Job: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."consul.hashicorp.com/config-checksum")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "serverACLInitCleanup/Job: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-cleanup-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
