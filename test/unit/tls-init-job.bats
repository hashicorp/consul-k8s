#!/usr/bin/env bats

load _helpers

@test "tlsInit/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-job.yaml  \
      .
}

@test "tlsInit/Job: disabled with global.tls.enabled=true and server.serverCert.secretName!=null" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'server.serverCert.secretName=test' \
      .
}

@test "tlsInit/Job: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enabled=false' \
      .
}

@test "tlsInit/Job: enabled with global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/Job: disabled when server.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      .
}

@test "tlsInit/Job: enabled when global.tls.enabled=true and server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/Job: sets additional IP SANs when provided and global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.serverAdditionalIPSANs[0]=1.1.1.1' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-additional-ipaddress=1.1.1.1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/Job: sets additional DNS SANs when provided and global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.serverAdditionalDNSSANs[0]=example.com' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-additional-dnsname=example.com"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/Job: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local spec=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo-ca-cert' \
      --set 'global.tls.caCert.secretKey=key' \
      --set 'global.tls.caKey.secretName=foo-ca-key' \
      --set 'global.tls.caKey.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec' | tee /dev/stderr)

  # check that the provided ca cert secret is attached as a volume
  local actual
  actual=$(echo $spec | jq -r '.volumes[] | select(.name=="consul-ca-cert") | .secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-cert" ]

  # uses the provided secret key for CA cert
  actual=$(echo $spec | jq -r '.volumes[] | select(.name=="consul-ca-cert") | .secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]

  # check that the provided ca key secret is attached as a volume
  local actual
  actual=$(echo $spec | jq -r '.volumes[] | select(.name=="consul-ca-key") | .secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-key" ]

  # uses the provided secret key for CA cert
  actual=$(echo $spec | jq -r '.volumes[] | select(.name=="consul-ca-key") | .secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]

  # check that it doesn't generate the CA
  actual=$(echo $spec | jq -r '.containers[0].command | join(" ") | contains("consul tls ca create")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
