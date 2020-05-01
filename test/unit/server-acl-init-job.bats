#!/usr/bin/env bats

load _helpers

@test "serverACLInit/Job: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: enabled with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: disabled with server=false and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: enabled with client=false global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: disabled when server.updatePartition > 0" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.updatePartition=1' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: enabled with externalServers.enabled=true global.acls.manageSystemACLs=true, but server.enabled set to false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
      --set 'server.enabled=true' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "only one of server.enabled or externalServers.enabled can be set" ]]
}

@test "serverACLInit/Job: fails if both externalServers.enabled=true and server.enabled not set to false" {
  cd `chart_dir`
  run helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "only one of server.enabled or externalServers.enabled can be set" ]]
}

@test "serverACLInit/Job: fails if createReplicationToken=true but manageSystemACLs=false" {
  cd `chart_dir`
  run helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.createReplicationToken=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "if global.acls.createReplicationToken is true, global.acls.manageSystemACLs must be true" ]]
}

@test "serverACLInit/Job: does not set -create-client-token=false when client is enabled (the default)" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2] | contains("-create-client-token=false")' |
      tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: sets -create-client-token=false when client is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2] | contains("-create-client-token=false")' |
      tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: server address is set to the DNS names of the server stateful set" {
  cd `chart_dir`
  local command=$(helm template \
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: dns acl option enabled with .dns.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: dns acl option disabled with .dns.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'dns.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# aclBindingRuleSelector/global.acls.manageSystemACLs

@test "serverACLInit/Job: no acl-binding-rule-selector flag by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml \
      --set 'connectInject.aclBindingRuleSlector=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: can specify acl-binding-rule-selector" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.aclBindingRuleSelector="foo"' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-binding-rule-selector=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# enterpriseLicense

@test "serverACLInit/Job: ent license acl option enabled with server.enterpriseLicense.secretName and server.enterpriseLicense.secretKey set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-enterprise-license-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: ent license acl option disabled missing server.enterpriseLicense.secretName" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-enterprise-license-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: ent license acl option disabled missing server.enterpriseLicense.secretKey" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-enterprise-license-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# client.snapshotAgent

@test "serverACLInit/Job: snapshot agent acl option disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-snapshot-agent-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: snapshot agent acl option enabled with .client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-snapshot-agent-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: mesh gateway acl option enabled with .meshGateway.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-mesh-gateway-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "serverACLInit/Job: sets TLS flags when global.tls.enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
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
# namespaces

@test "serverACLInit/Job: namespace options disabled by default" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
      -x templates/server-acl-init-job.yaml  \
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
    yq 'any(contains("create-inject-namespace-token"))' | tee /dev/stderr)
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
# global.acls.createReplicationToken

@test "serverACLInit/Job: -create-acl-replication-token is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-acl-replication-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: -create-acl-replication-token is true when acls.createReplicationToken is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.createReplicationToken=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-acl-replication-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.replicationToken

@test "serverACLInit/Job: -acl-replication-token-file is not set by default" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-job.yaml  \
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

@test "serverACLInit/Job: -acl-replication-token-file is not set when acls.replicationToken.secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretName=name' \
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

@test "serverACLInit/Job: -acl-replication-token-file is not set when acls.replicationToken.secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretKey=key' \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.hosts must be set if externalServers.enabled is true" ]]
}

@test "serverACLInit/Job: sets server address if externalServers.hosts are set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=false' \
      --set 'externalServers.hosts[0]=1.1.1.1' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-ca-cert=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.bootstrapToken

@test "serverACLInit/Job: -bootstrap-token-file is not set by default" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-job.yaml  \
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

@test "serverACLInit/Job: -bootstrap-token-file is not set when acls.bootstrapToken.secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \
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

@test "serverACLInit/Job: -bootstrap-token-file is not set when acls.bootstrapToken.secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.bootstrapToken.secretKey=key' \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
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
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-inject-auth-method-host"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: doesn't set auth method host by default when externalServers.k8sAuthMethodHost is provided but externalServers.enabled is false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.k8sAuthMethodHost=foo.com' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-inject-auth-method-host"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: can provide custom auth method host" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      --set 'externalServers.k8sAuthMethodHost=foo.com' \
      . | tee /dev/stderr|
      yq '.spec.template.spec.containers[0].command | any(contains("-inject-auth-method-host=foo.com"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
