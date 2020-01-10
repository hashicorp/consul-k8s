#!/usr/bin/env bats

load _helpers

@test "server/EnterpriseLicense: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: disabled when servers are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
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
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: disabled when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: enabled when secretName and secretKey is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
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
      -x templates/enterprise-license-job.yaml \
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
      -x templates/enterprise-license-job.yaml  \
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

#--------------------------------------------------------------------
# global.tls.enabled

@test "server/EnterpriseLicense: no volumes when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/EnterpriseLicense: volumes present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/EnterpriseLicense: no volumes mounted when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/EnterpriseLicense: volumes mounted when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/EnterpriseLicense: URL is http when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "http://release-name-consul-server:8500" ]
}

@test "server/EnterpriseLicense: URL is https when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "https://release-name-consul-server:8501" ]
}

@test "server/EnterpriseLicense: CA certificate is specified when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}
