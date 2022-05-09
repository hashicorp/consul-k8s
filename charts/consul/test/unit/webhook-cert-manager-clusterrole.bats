#!/usr/bin/env bats

load _helpers

@test "webhookCertManager/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      .
}

@test "webhookCertManager/ClusterRole: enabled with controller.enabled=true and connectInject.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/ClusterRole: enabled with connectInject.enabled=true and controller.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/ClusterRole: enabled with connectInject.enabled=true and controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      --set 'controller.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# rules

@test "webhookCertManager/ClusterRole: sets full access to secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]

  local actual=$(echo $object | yq -r '.apiGroups[0]' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object | yq -r '.verbs | index("create")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("delete")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("get")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("list")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("patch")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("update")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("watch")' | tee /dev/stderr)
  [ "${actual}" != null ]
}

@test "webhookCertManager/ClusterRole: sets get, list, watch, and patch access to mutatingwebhookconfigurations" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[1]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[0]' | tee /dev/stderr)
  [ "${actual}" = "mutatingwebhookconfigurations" ]

  local actual=$(echo $object | yq -r '.apiGroups[0]' | tee /dev/stderr)
  [ "${actual}" = "admissionregistration.k8s.io" ]

  local actual=$(echo $object | yq -r '.verbs | index("get")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("list")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("patch")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("watch")' | tee /dev/stderr)
  [ "${actual}" != null ]
}

@test "webhookCertManager/ClusterRole: sets get access to deployments" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[2]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[0]' | tee /dev/stderr)
  [ "${actual}" = "deployments" ]

  local actual=$(echo $object | yq -r '.apiGroups[0]' | tee /dev/stderr)
  [ "${actual}" = "apps" ]

  local actual=$(echo $object | yq -r '.resourceNames[0]' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-webhook-cert-manager" ]

  local actual=$(echo $object | yq -r '.verbs | index("get")' | tee /dev/stderr)
  [ "${actual}" != null ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "webhookCertManager/ClusterRole: allows podsecuritypolicies access for webhook-cert-manager with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[3]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]

  local actual=$(echo $object | yq -r '.resourceNames[0]' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-webhook-cert-manager" ]
}

#--------------------------------------------------------------------
# Vault

@test "webhookCertManager/ClusterRole: disabled when global.secretsBackend.vault.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-clusterrole.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      .
}
