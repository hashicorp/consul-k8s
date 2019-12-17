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
  [ "${actual}" = 'templates/server-acl-init-job.yaml:                -server-label-selector=component=server,app={{ template "consul.name" . }},release={{ .Release.Name }} \' ]
}
