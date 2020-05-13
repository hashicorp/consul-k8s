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
      -x templates/tests/test-runner.yaml \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-test" ]
}

@test "helper/consul.fullname: fullnameOverride overrides the name" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tests/test-runner.yaml \
      --set fullnameOverride=override \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "override-test" ]
}

@test "helper/consul.fullname: fullnameOverride is truncated to 63 chars" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tests/test-runner.yaml \
      --set fullnameOverride=abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijk-test" ]
}

@test "helper/consul.fullname: fullnameOverride has trailing '-' trimmed" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tests/test-runner.yaml \
      --set fullnameOverride=override- \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "override-test" ]
}

@test "helper/consul.fullname: global.name overrides the name" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tests/test-runner.yaml \
      --set global.name=override \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "override-test" ]
}

@test "helper/consul.fullname: global.name is truncated to 63 chars" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tests/test-runner.yaml \
      --set global.name=abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijk-test" ]
}

@test "helper/consul.fullname: global.name has trailing '-' trimmed" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tests/test-runner.yaml \
      --set global.name=override- \
      . | tee /dev/stderr |
      yq -r '.metadata.name' | tee /dev/stderr)
  [ "${actual}" = "override-test" ]
}

@test "helper/consul.fullname: nameOverride is supported" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tests/test-runner.yaml \
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
  local actual=$(grep -r '{{ .Release.Name }}' templates/*.yaml | grep -v 'release: ' | tee /dev/stderr )
  [ "${actual}" = '' ]
}

#--------------------------------------------------------------------
# consul.getAutoEncryptClientCA
# Similarly to consul.fullname tests, these tests use test-runner.yaml to test the
# consul.getAutoEncryptClientCA helper since we need an existing template that calls
# the consul.getAutoEncryptClientCA helper.

@test "helper/consul.getAutoEncryptClientCA: get-auto-encrypt-client-ca uses server's stateful set address by default" {
  cd `chart_dir`
  local command=$(helm template \
      -x templates/tests/test-runner.yaml  \
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
      -x templates/tests/test-runner.yaml  \
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
      -x templates/tests/test-runner.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.hosts must be set if externalServers.enabled is true" ]]
}

@test "helper/consul.getAutoEncryptClientCA: can set the provided port if externalServers.enabled is true" {
  cd `chart_dir`
  local command=$(helm template \
      -x templates/tests/test-runner.yaml  \
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

  # check server's CA cert
  actual=$(echo $command | jq ' . | contains("-ca-file=/consul/tls/ca/tls.crt")')
  [ "${actual}" = "true" ]
}

@test "helper/consul.getAutoEncryptClientCA: can pass cloud auto-join string to server address via externalServers.hosts" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tests/test-runner.yaml  \
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
      -x templates/tests/test-runner.yaml  \
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
      -x templates/tests/test-runner.yaml  \
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
      -x templates/tests/test-runner.yaml  \
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

#--------------------------------------------------------------------
# bootstrapACLs deprecation
#
# Test that every place global.bootstrapACLs is used, global.acls.manageSystemACLs
# is also used.
# If this test is failing, you've used either only global.bootstrapACLs or
# only global.acls.manageSystemACLs instead of using the backwards compatible:
#     or global.acls.manageSystemACLs global.bootstrapACLs
@test "helper/bootstrapACLs: used alongside manageSystemACLs" {
  cd `chart_dir`

  diff=$(diff <(grep -r '\.Values\.global\.bootstrapACLs' templates/*) <(grep -r -e 'or [\$root]*\.Values\.global\.acls\.manageSystemACLs [\$root]*\.Values\.global\.bootstrapACLs' templates/*) | tee /dev/stderr)
  [ "$diff" = "" ]

  diff=$(diff <(grep -r '\.Values\.global\.acls\.manageSystemACLs' templates/*) <(grep -r 'or [\$root]*\.Values\.global\.acls\.manageSystemACLs [\$root]*\.Values\.global\.bootstrapACLs' templates/*) | tee /dev/stderr)
  [ "$diff" = "" ]
}
