#!/usr/bin/env bats

load _helpers

@test "enterpriseLicense/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      .
}

@test "enterpriseLicense/Job: disabled if autoload is true (default) {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      .
}

@test "enterpriseLicense/Job: disabled when servers are disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enabled=false' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/Job: disabled when secretName is missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/Job: disabled when secretKey is missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/Job: enabled when secretName, secretKey is provided and autoload is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "enterpriseLicense/Job: fail is server.enterpriseLicense is set" {
  cd `chart_dir`
  run helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      .

      [ "$status" -eq 1 ]
      [[ "$output" =~ "server.enterpriseLicense has been moved to global.enterpriseLicense" ]]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "enterpriseLicense/Job: CONSUL_HTTP_TOKEN env variable created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "enterpriseLicense/Job: init container is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "ent-license-acl-init" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "enterpriseLicense/Job: no volumes when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "enterpriseLicense/Job: volumes present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "enterpriseLicense/Job: no volumes mounted when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "enterpriseLicense/Job: volumes mounted when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "enterpriseLicense/Job: URL is http when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "http://RELEASE-NAME-consul-server:8500" ]
}

@test "enterpriseLicense/Job: URL is https when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "https://RELEASE-NAME-consul-server:8501" ]
}

@test "enterpriseLicense/Job: CA certificate is specified when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "enterpriseLicense/Job: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/enterprise-license-job.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
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
