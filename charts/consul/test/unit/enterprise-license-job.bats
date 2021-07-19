#!/usr/bin/env bats

load _helpers

@test "server/EnterpriseLicense: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      .
}

@test "server/EnterpriseLicense: disabled if autoload is true (default) {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      .
}

@test "server/EnterpriseLicense: disabled when servers are disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "server/EnterpriseLicense: disabled when secretName is missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "server/EnterpriseLicense: disabled when secretKey is missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "server/EnterpriseLicense: enabled when secretName, secretKey is provided and autoload is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "server/EnterpriseLicense: CONSUL_HTTP_TOKEN env variable created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/EnterpriseLicense: init container is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.acls.manageSystemACLs=true' \
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
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/EnterpriseLicense: volumes present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/EnterpriseLicense: no volumes mounted when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/EnterpriseLicense: volumes mounted when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/EnterpriseLicense: URL is http when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "http://RELEASE-NAME-consul-server:8500" ]
}

@test "server/EnterpriseLicense: URL is https when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "https://RELEASE-NAME-consul-server:8501" ]
}

@test "server/EnterpriseLicense: CA certificate is specified when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "server/EnterpriseLicense: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo-ca-cert' \
      --set 'global.tls.caCert.secretKey=key' \
      --set 'global.tls.caKey.secretName=foo-ca-key' \
      --set 'global.tls.caKey.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name=="consul-ca-cert")' | tee /dev/stderr)

  # check that the provided ca cert secret is attached as a volume
  local actual
  actual=$(echo $ca_cert_volume | jq -r '.secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-cert" ]

  # check that the volume uses the provided secret key
  actual=$(echo $ca_cert_volume | jq -r '.secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]
}
