#!/usr/bin/env bats

load _helpers

@test "connectInject/ClusterRole: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/ClusterRole: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/ClusterRole: disabled with connectInject.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=false' \
      .
}

#--------------------------------------------------------------------
# rules

@test "connectInject/ClusterRole: sets get, list, and watch access to endpoints, services, namespaces and nodes in all api groups" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[2]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[| index("endpoints")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.resources[| index("services")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.resources[| index("namespaces")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.resources[| index("nodes")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.apiGroups[0]' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object | yq -r '.verbs | index("get")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("list")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("watch")' | tee /dev/stderr)
  [ "${actual}" != null ]
}

@test "connectInject/ClusterRole: sets get, list, watch and update access to pods in all api groups" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[4]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[| index("pods")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.apiGroups[0]' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object | yq -r '.verbs | index("get")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("list")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("watch")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("update")' | tee /dev/stderr)
  [ "${actual}" != null ]
}

@test "connectInject/ClusterRole: sets create, get, list, and update access to leases in the coordination.k8s.io api group" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules[5]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[| index("leases")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.apiGroups[0]' | tee /dev/stderr)
  [ "${actual}" = "coordination.k8s.io" ]

  local actual=$(echo $object | yq -r '.verbs | index("create")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("get")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("list")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("update")' | tee /dev/stderr)
  [ "${actual}" != null ]
}

@test "connectInject/ClusterRole: sets get access to serviceaccounts and secrets when manageSystemACLSis true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[2]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[| index("serviceaccounts")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.resources[| index("secrets")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.apiGroups[0]' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object | yq -r '.verbs | index("get")' | tee /dev/stderr)
  [ "${actual}" != null ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "connectInject/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "connectInject/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# vault

@test "connectInject/ClusterRole: vault sets get, list, watch, and patch access to mutatingwebhookconfigurations when the following are configured - global.secretsBackend.vault.enabled, global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, and global.secretsBackend.vault.connectInject.caCert.secretName." {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.connectInjectRole=inject-ca-role' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      . | tee /dev/stderr |
      yq -r '.rules[6]' | tee /dev/stderr)

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

#--------------------------------------------------------------------
# openshift

@test "connectInject/ClusterRole: adds permission to securitycontextconstraints for Openshift with global.openshift.enabled=true with default apiGateway Openshift SCC Name" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq '.rules[13].resourceNames | index("restricted-v2")' | tee /dev/stderr)
  [ "${object}" == 0 ]
}

@test "connectInject/ClusterRole: adds permission to securitycontextconstraints for Openshift with global.openshift.enabled=true and sets apiGateway Openshift SCC Name" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-clusterrole.yaml  \
      --set 'global.openshift.enabled=true' \
      --set 'connectInject.apiGateway.managedGatewayClass.openshiftSCCName=fakescc' \
      . | tee /dev/stderr |
       yq '.rules[13].resourceNames | index("fakescc")' | tee /dev/stderr)
   [ "${object}" == 0 ]
}
