#!/usr/bin/env bats

load _helpers

@test "server/EnterpriseLicense: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: disabled when servers are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: disabled when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: disabled when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: enabled when secretName and secretKey is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.bootstrapACLs

@test "server/EnterpriseLicense: CONSUL_HTTP_TOKEN env variable created when global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/EnterpriseLicense: init container is created when global.bootstrapACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "ent-license-acl-init" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/EnterpriseLicense: no service account specified when global.bootstrapACLS=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.serviceAccountName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/EnterpriseLicense: service account specified when global.bootstrapACLS=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.serviceAccountName | contains("consul-enterprise-license")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
