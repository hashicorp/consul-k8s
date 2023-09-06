#!/usr/bin/env bats
# This file tests the helpers in _helpers.tpl.

load _helpers

#--------------------------------------------------------------------
# consul.fullname
# These tests use test-runner.yaml to test the consul.fullname helper
# since we need an existing template that calls the consul.fullname helper.

@test "helper/consul.fullname: defaults to release-name-consul" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-test" ]
}

@test "helper/consul.fullname: fullnameOverride overrides the name" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml \
      --set fullnameOverride=override \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "override-test" ]
}

@test "helper/consul.fullname: fullnameOverride is truncated to 63 chars" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml \
      --set fullnameOverride=abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijk-test" ]
}

@test "helper/consul.fullname: fullnameOverride has trailing '-' trimmed" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml \
      --set fullnameOverride=override- \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "override-test" ]
}

@test "helper/consul.fullname: global.name overrides the name" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml \
      --set global.name=override \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "override-test" ]
}

@test "helper/consul.fullname: global.name is truncated to 63 chars" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml \
      --set global.name=abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijk-test" ]
}

@test "helper/consul.fullname: global.name has trailing '-' trimmed" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml \
      --set global.name=override- \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "override-test" ]
}

@test "helper/consul.fullname: nameOverride is supported" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml \
      --set nameOverride=override \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-override-test" ]
}

#--------------------------------------------------------------------
# template consul.fullname
#
# This test ensures that we use {{ template "consul.fullname" }} everywhere instead of
# {{ .Release.Name }} because that's required in order to support the name
# override settings fullnameOverride and global.name. In some cases, we need to
# use .Release.Name. In those cases, add your exception to this list.
#
# If this test fails, you're likely using {{ .Release.Name }} where you should
# be using {{ template "consul.fullname" }}
@test "helper/consul.fullname: used everywhere" {
  cd `chart_dir`
  # Grep for uses of .Release.Name that aren't using it as a label.
  local actual=$(grep -r '{{ .Release.Name }}' templates/*.yaml | grep -v -E 'release: |-release-name=' | tee /dev/stderr )
  [ "${actual}" = '' ]
}

#--------------------------------------------------------------------
# template namespace
#
# This test ensures that we set "namespace: " in every file. The exceptions are files with CRDs and clusterroles and
# clusterrolebindings.
#
# If this test fails, you're likely missing setting the namespace.

@test "helper/namespace: used everywhere" {
  cd `chart_dir`
  # Grep for files that don't have 'namespace: ' in them
  local actual=$(grep -L 'namespace: ' templates/*.yaml | grep -v 'crd' | grep -v 'clusterrole' | grep -v 'gateway-gateway' | tee /dev/stderr )
  [ "${actual}" = '' ]
}

#--------------------------------------------------------------------
# component label
#
# This test ensures that we set a "component: <blah>" in every file.
#
# If this test fails, you're likely missing setting that label somewhere.

@test "helper/component-label: used everywhere" {
  cd `chart_dir`
  # Grep for files that don't have 'component: ' in them
  local actual=$(grep -L 'component: ' templates/*.yaml | tee /dev/stderr )
  [ "${actual}" = '' ]
}

#--------------------------------------------------------------------
# consul.getAutoEncryptClientCA
# Similarly to consul.fullname tests, these tests use test-runner.yaml to test the
# consul.getAutoEncryptClientCA helper since we need an existing template that calls
# the consul.getAutoEncryptClientCA helper.

@test "helper/consul.getAutoEncryptClientCA: get-auto-encrypt-client-ca uses server's stateful set address by default and passes ca cert" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").command | join(" ")' | tee /dev/stderr)

  # check server address
  actual=$(echo $command | jq ' . | contains("-server-addr=release-name-consul-server")')
  [ "${actual}" = "true" ]

  # check server port
  actual=$(echo $command | jq ' . | contains("-server-port=8501")')
  [ "${actual}" = "true" ]

  # check server's CA cert
  actual=$(echo $command | jq ' . | contains("-ca-file=/consul/tls/ca/tls.crt")')
  [ "${actual}" = "true" ]
}

@test "helper/consul.getAutoEncryptClientCA: can set the provided server hosts if externalServers.enabled is true" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.io' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").command | join(" ")' | tee /dev/stderr)

  # check server address
  actual=$(echo $command | jq ' . | contains("-server-addr=\"consul.io\"")')
  [ "${actual}" = "true" ]

  # check the default server port is 443 if not provided
  actual=$(echo $command | jq ' . | contains("-server-port=8501")')
  [ "${actual}" = "true" ]

  # check server's CA cert
  actual=$(echo $command | jq ' . | contains("-ca-file=/consul/tls/ca/tls.crt")')
  [ "${actual}" = "true" ]
}

@test "helper/consul.getAutoEncryptClientCA: fails if externalServers.enabled is true but externalServers.hosts are not provided" {
  cd `chart_dir`
  run helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.hosts must be set if externalServers.enabled is true" ]]
}

@test "helper/consul.getAutoEncryptClientCA: can set the provided port if externalServers.enabled is true" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.io' \
      --set 'externalServers.httpsPort=443' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").command | join(" ")' | tee /dev/stderr)

  # check server address
  actual=$(echo $command | jq ' . | contains("-server-addr=\"consul.io\"")')
  [ "${actual}" = "true" ]

  # check the default server port is 443 if not provided
  actual=$(echo $command | jq ' . | contains("-server-port=443")')
  [ "${actual}" = "true" ]
}

@test "helper/consul.getAutoEncryptClientCA: can pass cloud auto-join string to server address via externalServers.hosts" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=provider=my-cloud config=val' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").command | any(contains("-server-addr=\"provider=my-cloud config=val\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "helper/consul.getAutoEncryptClientCA: can set TLS server name if externalServers.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.io' \
      --set 'externalServers.tlsServerName=custom-server-name' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").command | join(" ") | contains("-tls-server-name=custom-server-name")' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "helper/consul.getAutoEncryptClientCA: doesn't provide the CA if externalServers.enabled is true and externalServers.useSystemRoots is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.io' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").command | join(" ") | contains("-ca-file=/consul/tls/ca/tls.crt")' | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "helper/consul.getAutoEncryptClientCA: doesn't mount the consul-ca-cert volume if externalServers.enabled is true and externalServers.useSystemRoots is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.io' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").volumeMounts[] | select(.name=="consul-ca-cert")' | tee /dev/stderr)

  [ "${actual}" = "" ]
}

@test "helper/consul.getAutoEncryptClientCA: uses the correct -ca-file when vault is enabled and external servers disabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/ca/pem' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca")' | tee /dev/stderr)

  actual=$(echo $object | jq '.command | join(" ") | contains("-ca-file=/vault/secrets/serverca.crt")')
  [ "${actual}" = "true" ]

  actual=$(echo $object | jq '.volumeMounts[] | select(.name == "consul-ca-cert")')
  [ "${actual}" = "" ]
}

@test "helper/consul.getAutoEncryptClientCA: uses the correct -ca-file when vault and external servers is enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/ca/pem' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul.io' \
      . | tee /dev/stderr |
      yq '.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca")' | tee /dev/stderr)

  actual=$(echo $object | jq '.command | join(" ") | contains("-ca-file=/vault/secrets/serverca.crt")')
  [ "${actual}" = "true" ]

  actual=$(echo $object | jq '.volumeMounts[] | select(.name == "consul-ca-cert")')
  [ "${actual}" = "" ]
}
