#!/usr/bin/env bats
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

load _helpers

# Match the three security configurations in acceptance/tests/basic.
assert_basic_openshift() {
  local secure="$1"
  local auto_encrypt="$2"
  cd "$(chart_dir)"
  run helm template \
    --set 'global.openshift.enabled=true' \
    --set 'global.installK8sNetworkingCRDs=false' \
    --set 'connectInject.enabled=false' \
    --set 'client.enabled=true' \
    --set 'server.replicas=1' \
    --set "global.acls.manageSystemACLs=${secure}" \
    --set "global.tls.enabled=${secure}" \
    --set "global.gossipEncryption.autoGenerate=${secure}" \
    --set "global.tls.enableAutoEncrypt=${auto_encrypt}" .
  [ "$status" -eq 0 ]
  for template in server-statefulset client-daemonset; do
    echo "$output" | grep -q "# Source: consul/templates/${template}.yaml"
  done
  for template in connect-inject-deployment webhook-cert-manager-deployment gateway-resources-job gateway-resources-job-custom; do
    ! echo "$output" | grep -q "# Source: consul/templates/${template}.yaml"
  done
  ! echo "$output" | grep -q '^kind: CustomResourceDefinition$'
  for template in server-acl-init-job tls-init-job gossip-encryption-autogenerate-job; do
    if [ "$secure" = true ]; then
      echo "$output" | grep -q "# Source: consul/templates/${template}.yaml"
    else
      ! echo "$output" | grep -q "# Source: consul/templates/${template}.yaml"
    fi
  done
  if [ "$auto_encrypt" = true ]; then
    echo "$output" | grep -q '"auto_encrypt"'
  fi
}

@test "basic/OpenShift: server and client installation without injector" {
  assert_basic_openshift false false
}

@test "basic/OpenShift: ACL TLS and gossip jobs retained without injector" {
  assert_basic_openshift true false
}

@test "basic/OpenShift: auto-encryption retained without injector" {
  assert_basic_openshift true true
}

