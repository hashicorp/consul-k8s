#!/usr/bin/env bats

load _helpers

@test "serverACLInit/Job: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: enabled with global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: disabled with server=false and global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: disabled with client=false and global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# dns

@test "serverACLInit/Job: dns acl option enabled with .dns.enabled=-" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: dns acl option enabled with .dns.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: dns acl option disabled with .dns.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'dns.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# aclBindingRuleSelector/global.bootstrapACLs

@test "serverACLInit/Job: no acl-binding-rule-selector flag by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml \
      --set 'connectInject.aclBindingRuleSlector=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: can specify acl-binding-rule-selector" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      --set 'connectInject.aclBindingRuleSelector="foo"' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-binding-rule-selector=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# enterpriseLicense

@test "serverACLInit/Job: ent license acl option enabled with server.enterpriseLicense.secretName and server.enterpriseLicense.secretKey set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-enterprise-license-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: ent license acl option disabled missing server.enterpriseLicense.secretName" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-enterprise-license-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: ent license acl option disabled missing server.enterpriseLicense.secretKey" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-enterprise-license-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# client.snapshotAgent

@test "serverACLInit/Job: snapshot agent acl option disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-snapshot-agent-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: snapshot agent acl option enabled with .client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-snapshot-agent-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: mesh gateway acl option enabled with .meshGateway.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-mesh-gateway-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
