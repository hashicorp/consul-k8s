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

@test "tlsInit/Job: sets additional DNS SANs by default when global.tls.enabled=true" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$command" |
    yq 'any(contains("additional-dnsname=\"release-name-consul-server\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$command" |
    yq 'any(contains("additional-dnsname=\"*.release-name-consul-server\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$command" |
    yq 'any(contains("additional-dnsname=\"*.release-name-consul-server.${NAMESPACE}\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$command" |
    yq 'any(contains("additional-dnsname=\"release-name-consul-server.${NAMESPACE}\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$command" |
    yq 'any(contains("additional-dnsname=\"*.release-name-consul-server.${NAMESPACE}.svc\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$command" |
    yq 'any(contains("additional-dnsname=\"release-name-consul-server.${NAMESPACE}.svc\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$command" |
    yq 'any(contains("additional-dnsname=\"*.server.dc1.consul\""))' | tee /dev/stderr)
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

#--------------------------------------------------------------------
# Vault

@test "tlsInit/Job: disabled with global.secretsBackend.vault.enabled=true and global.tls.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'global.tls.enableAutoEncrypt=true' \
      .
}

#--------------------------------------------------------------------
# extraLabels

@test "tlsInit/Job: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "tlsInit/Job: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "tlsInit/Job: multiple extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
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

@test "tlsInit/Job: use global.logLevel by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/Job: override global.logLevel when global.tls.logLevel is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.logLevel=error' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=error"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# server.containerSecurityContext.tlsInit

@test "tlsInit/Job: securityContext is set when server.containerSecurityContext.tlsInit is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.containerSecurityContext.tlsInit.runAsUser=100' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext.runAsUser' | tee /dev/stderr)

  [ "${actual}" = "100" ]
}

#--------------------------------------------------------------------
# annotations

@test "tlsInit/Job: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."consul.hashicorp.com/config-checksum")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "tlsInit/Job: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
