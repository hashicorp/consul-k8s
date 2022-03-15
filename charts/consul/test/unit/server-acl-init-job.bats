#!/usr/bin/env bats

load _helpers

@test "serverACLInit/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-acl-init-job.yaml  \
      .
}

@test "serverACLInit/Job: enabled with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: disabled with server=false and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      .
}

@test "serverACLInit/Job: enabled with client=false global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: disabled when server.updatePartition > 0" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.updatePartition=1' \
      .
}

@test "serverACLInit/Job: enabled with externalServers.enabled=true global.acls.manageSystemACLs=true, but server.enabled set to false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'server.enabled=false' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: fails if both externalServers.enabled=true and server.enabled=true" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'server.enabled=true' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "only one of server.enabled or externalServers.enabled can be set" ]]
}

@test "serverACLInit/Job: fails if both externalServers.enabled=true and server.enabled not set to false" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "only one of server.enabled or externalServers.enabled can be set" ]]
}

@test "serverACLInit/Job: fails if createReplicationToken=true but manageSystemACLs=false" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.createReplicationToken=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "if global.acls.createReplicationToken is true, global.acls.manageSystemACLs must be true" ]]
}

# We removed bootstrapACLs, and now fail in case anyone is still using it.
@test "serverACLInit/Job: fails if global.bootstrapACLs is true" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.bootstrapACLs was removed, use global.acls.manageSystemACLs instead" ]]
}

@test "serverACLInit/Job: does not set -client=false when client is enabled (the default)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2] | contains("-client=false")' |
      tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: sets -client=false when client is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2] | contains("-client=false")' |
      tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: server address is set to the DNS names of the server stateful set" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | any(contains("-server-address=\"${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | any(contains("-server-address=\"${CONSUL_FULLNAME}-server-1.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | any(contains("-server-address=\"${CONSUL_FULLNAME}-server-2.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# dns

@test "serverACLInit/Job: dns acl option enabled with .dns.enabled=-" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: dns acl option enabled with .dns.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: dns acl option disabled with .dns.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'dns.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# aclBindingRuleSelector/global.acls.manageSystemACLs

@test "serverACLInit/Job: acl-binding-rule-selector flag set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-binding-rule-selector=serviceaccount.name!=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: can specify acl-binding-rule-selector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.aclBindingRuleSelector="foo"' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-binding-rule-selector=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# enterpriseLicense

@test "serverACLInit/Job: ent license acl option enabled with global.enterpriseLicense.secretName and global.enterpriseLicense.secretKey set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-enterprise-license-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# client.snapshotAgent

@test "serverACLInit/Job: snapshot agent acl option disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-snapshot-agent"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: snapshot agent acl option enabled with .client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-snapshot-agent"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# syncCatalog.enabled

@test "serverACLInit/Job: sync catalog acl option disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-sync-catalog"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: sync catalog acl option enabled with .syncCatalog.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-sync-catalog"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: sync catalog node name set to 'k8s-sync' by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-sync-consul-node-name=k8s-sync"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: sync catalog node name set to 'k8s-sync' can be changed" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.consulNodeName=new-node-name' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-sync-consul-node-name=new-node-name"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# meshGateway.enabled

@test "serverACLInit/Job: mesh gateway acl option disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-mesh-gateway"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: mesh gateway acl option enabled with .meshGateway.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-mesh-gateway"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# ingressGateways.enabled

@test "serverACLInit/Job: ingress gateways acl options disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-ingress-gateway-name"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: ingress gateways acl option enabled with .ingressGateways.enabled=true (single default gateway)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-ingress-gateway-name=\"ingress-gateway\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: able to define multiple ingress gateways" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[1].name=gateway2' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'contains("-ingress-gateway-name=\"gateway1\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'contains("-ingress-gateway-name=\"gateway2\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'indices("-ingress-gateway-name") | length' | tee /dev/stderr)
  [ "${actual}" = 2 ]
}

@test "serverACLInit/Job: ingress gateways acl option enabled with .ingressGateways.enabled=true, namespaces enabled, default namespace" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-ingress-gateway-name=\"ingress-gateway.default\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: ingress gateways acl option enabled with .ingressGateways.enabled=true, namespaces enabled, no default namespace set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'ingressGateways.defaults.consulNamespace=' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-ingress-gateway-name=\"ingress-gateway\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: multiple ingress gateways with namespaces enabled provides the correct flag format" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'ingressGateways.defaults.consulNamespace=default-namespace' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[1].name=gateway2' \
      --set 'ingressGateways.gateways[1].consulNamespace=namespace2' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'contains("-ingress-gateway-name=\"gateway1.default-namespace\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'contains("-ingress-gateway-name=\"gateway2.namespace2\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'indices("-ingress-gateway-name") | length' | tee /dev/stderr)
  [ "${actual}" = 2 ]
}

#--------------------------------------------------------------------
# terminatingGateways.enabled

@test "serverACLInit/Job: terminating gateways acl options disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-terminating-gateway-name"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: terminating gateways acl option enabled with .terminatingGateways.enabled=true (single default gateway)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-terminating-gateway-name=\"terminating-gateway\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: able to define multiple terminating gateways" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[1].name=gateway2' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'contains("-terminating-gateway-name=\"gateway1\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'contains("-terminating-gateway-name=\"gateway2\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'indices("-terminating-gateway-name") | length' | tee /dev/stderr)
  [ "${actual}" = 2 ]
}

@test "serverACLInit/Job: terminating gateways acl option enabled with .terminatingGateways.enabled=true, namespaces enabled, default namespace" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-terminating-gateway-name=\"terminating-gateway.default\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: terminating gateways acl option enabled with .terminatingGateways.enabled=true, namespaces enabled, no default namespace set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'terminatingGateways.defaults.consulNamespace=' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-terminating-gateway-name=\"terminating-gateway\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: multiple terminating gateways with namespaces enabled provides the correct flag format" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'terminatingGateways.defaults.consulNamespace=default-namespace' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[1].name=gateway2' \
      --set 'terminatingGateways.gateways[1].consulNamespace=namespace2' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2]' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'contains("-terminating-gateway-name=\"gateway1.default-namespace\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'contains("-terminating-gateway-name=\"gateway2.namespace2\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'indices("-terminating-gateway-name") | length' | tee /dev/stderr)
  [ "${actual}" = 2 ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "serverACLInit/Job: sets TLS flags when global.tls.enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | any(contains("-use-https"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | any(contains("-consul-ca-cert=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | any(contains("-server-port=8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
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

#--------------------------------------------------------------------
# Vault

@test "serverACLInit/Job: fails when vault is enabled but neither bootstrap nor replication token is provided" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.acls.bootstrapToken or global.acls.replicationToken must be provided when global.secretsBackend.vault.enabled and global.acls.manageSystemACLs are true" ]]
}

@test "serverACLInit/Job: fails when vault is enabled but manageSystemACLsRole is not provided" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \
      --set 'global.acls.bootstrapToken.secretKey=key' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.secretsBackend.vault.manageSystemACLsRole is required when global.secretsBackend.vault.enabled and global.acls.manageSystemACLs are true" ]]
}

@test "serverACLInit/Job: configures vault annotations and bootstrap token secret by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=foo' \
      --set 'global.acls.bootstrapToken.secretKey=bar' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check annotations
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-pre-populate-only"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "aclrole" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-secret-bootstrap-token"')
  [ "${actual}" = "foo" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-template-bootstrap-token"')
  local expected=$'{{- with secret \"foo\" -}}\n{{- .Data.data.bar -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  # Check that the bootstrap token flag is set to the path of the Vault secret.
  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name="post-install-job").command | any(contains("-bootstrap-token-file=/vault/secrets/bootstrap-token"))')
  [ "${actual}" = "true" ]

  # Check that no (secret) volumes are not attached
  local actual=$(echo $object | jq -r '.spec.volumes')
  [ "${actual}" = "null" ]

  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name="post-install-job").volumeMounts')
  [ "${actual}" = "null" ]
}

@test "serverACLInit/Job: configures server CA to come from vault when vault and TLS are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=foo' \
      --set 'global.acls.bootstrapToken.secretKey=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.server.serverCert.secretName=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
      . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check annotations
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-pre-populate-only"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "aclrole" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = $'{{- with secret \"foo\" -}}\n{{- .Data.certificate -}}\n{{- end -}}' ]

  # Check that the consul-ca-cert volume is not attached
  local actual=$(echo $object | jq -r '.spec.volumes')
  [ "${actual}" = "null" ]

  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name="post-install-job").volumeMounts')
  [ "${actual}" = "null" ]
}

@test "serverACLInit/Job: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-acl-init-job.yaml  \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.acls.bootstrapToken.secretName=foo' \
    --set 'global.acls.bootstrapToken.secretKey=bar' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.server.serverCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-acl-init-job.yaml  \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.acls.bootstrapToken.secretName=foo' \
    --set 'global.acls.bootstrapToken.secretKey=bar' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.server.serverCert.secretName=foo' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-acl-init-job.yaml  \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.acls.bootstrapToken.secretName=foo' \
    --set 'global.acls.bootstrapToken.secretKey=bar' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.server.serverCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-acl-init-job.yaml  \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.acls.bootstrapToken.secretName=foo' \
    --set 'global.acls.bootstrapToken.secretKey=bar' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.server.serverCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

#--------------------------------------------------------------------
# Replication token in Vault

@test "serverACLInit/Job: vault replication token can be provided" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-acl-init-job.yaml  \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
    --set 'global.acls.replicationToken.secretName=/vault/secret' \
    --set 'global.acls.replicationToken.secretKey=token' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check that the role is set.
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/role"')
  [ "${actual}" = "acl-role" ]

  # Check Vault secret annotations.
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-secret-replication-token"')
  [ "${actual}" = "/vault/secret" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-template-replication-token"')
  local expected=$'{{- with secret \"/vault/secret\" -}}\n{{- .Data.data.token -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  # Check that replication token Kubernetes secret volumes and volumeMounts are not attached.
  local actual=$(echo $object | jq -r '.spec.volumes')
  [ "${actual}" = "null" ]

  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name="post-install-job").volumeMounts')
  [ "${actual}" = "null" ]

  # Check that the replication token flag is set to the path of the Vault secret.
  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name="post-install-job").command | any(contains("-acl-replication-token-file=/vault/secrets/replication-token"))')
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: both replication and bootstrap tokens can be provided together" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-acl-init-job.yaml  \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.manageSystemACLsRole=acl-role' \
    --set 'global.acls.replicationToken.secretName=/vault/replication' \
    --set 'global.acls.replicationToken.secretKey=token' \
    --set 'global.acls.bootstrapToken.secretName=/vault/bootstrap' \
    --set 'global.acls.bootstrapToken.secretKey=token' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check that the role is set.
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/role"')
  [ "${actual}" = "acl-role" ]

  # Check Vault secret annotations.
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-secret-replication-token"')
  [ "${actual}" = "/vault/replication" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-template-replication-token"')
  local expected=$'{{- with secret \"/vault/replication\" -}}\n{{- .Data.data.token -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-secret-bootstrap-token"')
  [ "${actual}" = "/vault/bootstrap" ]

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-inject-template-bootstrap-token"')
  local expected=$'{{- with secret \"/vault/bootstrap\" -}}\n{{- .Data.data.token -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  # Check that replication token Kubernetes secret volumes and volumeMounts are not attached.
  local actual=$(echo $object | jq -r '.spec.volumes')
  [ "${actual}" = "null" ]

  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name="post-install-job").volumeMounts')
  [ "${actual}" = "null" ]

  # Check that the replication and bootstrap token flags are set to the path of the Vault secret.
  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name="post-install-job").command | any(contains("-acl-replication-token-file=/vault/secrets/replication-token"))')
  [ "${actual}" = "true" ]

  local actual=$(echo $object | jq -r '.spec.containers[] | select(.name="post-install-job").command | any(contains("-bootstrap-token-file=/vault/secrets/bootstrap-token"))')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault agent annotations

@test "serverACLInit/Job: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=foo' \
      --set 'global.acls.bootstrapToken.secretKey=bar' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations | del(."consul.hashicorp.com/connect-inject") | del(."vault.hashicorp.com/agent-inject") | del(."vault.hashicorp.com/agent-pre-populate-only") | del(."vault.hashicorp.com/role") | del(."vault.hashicorp.com/agent-inject-secret-bootstrap-token") | del(."vault.hashicorp.com/agent-inject-template-bootstrap-token")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "serverACLInit/Job: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=foo' \
      --set 'global.acls.bootstrapToken.secretKey=bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# namespaces

@test "serverACLInit/Job: namespace options disabled by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# namespaces + sync

@test "serverACLInit/Job: sync namespace options not set with namespaces enabled, sync disabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'syncCatalog.consulNamespaces.mirroringK8S=true' \
      --set 'syncCatalog.consulNamespaces.mirroringK8SPrefix=k8s-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: sync namespace options set with .global.enableConsulNamespaces=true and sync enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: sync mirroring options set with .syncCatalog.consulNamespaces.mirroringK8S=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.consulNamespaces.mirroringK8S=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: sync prefix can be set with .syncCatalog.consulNamespaces.mirroringK8SPrefix" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.consulNamespaces.mirroringK8S=true' \
      --set 'syncCatalog.consulNamespaces.mirroringK8SPrefix=k8s-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix=k8s-"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# namespaces + inject

@test "serverACLInit/Job: inject namespace options not set with namespaces enabled, inject disabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      --set 'connectInject.consulNamespaces.mirroringK8SPrefix=k8s-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: inject namespace options set with .global.enableConsulNamespaces=true and inject enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: inject mirroring options set with .connectInject.consulNamespaces.mirroringK8S=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: inject prefix can be set with .connectInject.consulNamespaces.mirroringK8SPrefix" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      --set 'connectInject.consulNamespaces.mirroringK8SPrefix=k8s-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-sync-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-sync-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("sync-k8s-namespace-mirroring-prefix=k8s-"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("connect-inject"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-inject-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-inject-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("inject-k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# admin partitions

@test "serverACLInit/Job: admin partitions disabled by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-partitions"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("partition"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: admin partitions enabled when admin partitions are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-partitions"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("partition"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.createReplicationToken

@test "serverACLInit/Job: -create-acl-replication-token is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-acl-replication-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: -create-acl-replication-token is true when acls.createReplicationToken is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.createReplicationToken=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-acl-replication-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.replicationToken

@test "serverACLInit/Job: replicationToken.secretKey is required when replicationToken.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretName=name' \ .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "both global.acls.replicationToken.secretKey and global.acls.replicationToken.secretName must be set if one of them is provided" ]]
}

@test "serverACLInit/Job: replicationToken.secretName is required when replicationToken.secretKey is set" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretKey=key' \ .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "both global.acls.replicationToken.secretKey and global.acls.replicationToken.secretName must be set if one of them is provided" ]]
}

@test "serverACLInit/Job: -acl-replication-token-file is not set by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("-acl-replication-token-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the volume doesn't exist
  local actual=$(echo "$object" |
    yq '.spec.template.spec.volumes | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount doesn't exist
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].volumeMounts | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: -acl-replication-token-file is set when acls.replicationToken.secretKey and secretName are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretName=name' \
      --set 'global.acls.replicationToken.secretKey=key' \
      . | tee /dev/stderr)

  # Test the -acl-replication-token-file flag is set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("-acl-replication-token-file=/consul/acl/tokens/acl-replication-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume exists
  local actual=$(echo "$object" |
    yq '.spec.template.spec.volumes | map(select(.name == "acl-replication-token")) | length == 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount exists
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].volumeMounts | map(select(.name == "acl-replication-token")) | length == 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# externalServers.enabled

@test "serverACLInit/Job: fails if external servers are enabled but externalServers.hosts are not set" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.hosts must be set if externalServers.enabled is true" ]]
}

@test "serverACLInit/Job: sets server address if externalServers.hosts are set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-server-address=\"foo.com\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: can pass cloud auto-join string to server address via externalServers.hosts" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=provider=my-cloud config=val' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-server-address=\"provider=my-cloud config=val\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: port 8501 is used by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=1.1.1.1' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-server-port=8501"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: can override externalServers.httpsPort" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=1.1.1.1' \
      --set 'externalServers.httpsPort=443' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-server-port=443"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: uses only the port from externalServers.httpsPort if TLS is enabled and externalServers.enabled is false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=1.1.1.1' \
      --set 'externalServers.httpsPort=443' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-server-port=8501"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: doesn't set the CA cert if TLS is enabled and externalServers.useSystemRoots is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=1.1.1.1' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-ca-cert=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: sets the CA cert if TLS is enabled and externalServers.enabled is true but externalServers.useSystemRoots is false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=1.1.1.1' \
      --set 'externalServers.useSystemRoots=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-ca-cert=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: sets the CA cert if TLS is enabled and externalServers.useSystemRoots is true but externalServers.enabled is false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=false' \
      --set 'externalServers.hosts[0]=1.1.1.1' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-ca-cert=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: sets TLS server name if externalServers.tlsServerName is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.tlsServerName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-tls-server-name=foo"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.bootstrapToken

@test "serverACLInit/Job: bootstrapToken.secretKey is required when bootstrapToken.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \ .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "both global.acls.bootstrapToken.secretKey and global.acls.bootstrapToken.secretName must be set if one of them is provided" ]]
}

@test "serverACLInit/Job: bootstrapToken.secretName is required when bootstrapToken.secretKey is set" {
  cd `chart_dir`
  run helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretKey=key' \ .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "both global.acls.bootstrapToken.secretKey and global.acls.bootstrapToken.secretName must be set if one of them is provided" ]]
}

@test "serverACLInit/Job: -bootstrap-token-file is not set by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].command | any(contains("-bootstrap-token-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the volume doesn't exist
  local actual=$(echo "$object" |
  yq '.spec.template.spec.volumes | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount doesn't exist
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].volumeMounts | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: -bootstrap-token-file is set when acls.bootstrapToken.secretKey and secretName are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \
      --set 'global.acls.bootstrapToken.secretKey=key' \
      . | tee /dev/stderr)

  # Test the -bootstrap-token-file flag is set.
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].command | any(contains("-bootstrap-token-file=/consul/acl/tokens/bootstrap-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume exists
  local actual=$(echo "$object" |
  yq '.spec.template.spec.volumes | map(select(.name == "bootstrap-token")) | length == 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount exists
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].volumeMounts | map(select(.name == "bootstrap-token")) | length == 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: -bootstrap-token-file is preferred when both acls.bootstrapToken and acls.replicationToken are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \
      --set 'global.acls.bootstrapToken.secretKey=key' \
      --set 'global.acls.replicationToken.secretName=replication' \
      --set 'global.acls.replicationToken.secretKey=token' \
      . | tee /dev/stderr)

  # Test the -bootstrap-token-file flag is set.
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].command | any(contains("-bootstrap-token-file=/consul/acl/tokens/bootstrap-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume exists
  local actual=$(echo "$object" |
  yq '.spec.template.spec.volumes | map(select(.name == "bootstrap-token")) | length == 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the volume mount exists
  local actual=$(echo "$object" |
  yq '.spec.template.spec.containers[0].volumeMounts | map(select(.name == "bootstrap-token")) | length == 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: doesn't set auth method host by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-auth-method-host"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: doesn't set auth method host by default when externalServers.k8sAuthMethodHost is provided but externalServers.enabled is false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.k8sAuthMethodHost=foo.com' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-auth-method-host"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: can provide custom auth method host for external servers" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      --set 'externalServers.k8sAuthMethodHost=foo.com' \
      . | tee /dev/stderr|
      yq '.spec.template.spec.containers[0].command | any(contains("-auth-method-host=foo.com"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: can provide custom auth method host for federation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'global.federation.k8sAuthMethodHost=foo.com' \
      --set 'meshGateway.enabled=true' \
      . | tee /dev/stderr|
      yq '.spec.template.spec.containers[0].command | any(contains("-auth-method-host=foo.com"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# controller

@test "serverACLInit/Job: -controller not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("controller"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: -controller set when controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("controller"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.federation.enabled

@test "serverACLInit/Job: ensure federation is passed when federation is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-federation"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
