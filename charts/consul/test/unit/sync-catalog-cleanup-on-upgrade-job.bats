#!/usr/bin/env bats

load _helpers

target=templates/sync-catalog-cleanup-on-upgrade-job.yaml

@test "syncCatalogCleanupJob/Upgrade: disabled by default" {
  cd $(chart_dir)
  assert_empty helm template \
    -s $target \
    .
}

@test "syncCatalogCleanupJob/Upgrade: disabled with syncCatalog.cleanupNodeOnRemoval true and syncCatalog.enabled true and Release.IsUpgrade true" {
  cd $(chart_dir)
  assert_empty helm template \
    -s $target \
    --set 'syncCatalog.enabled=true' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    .
}

@test "syncCatalogCleanupJob/Upgrade: enable with syncCatalog.cleanupNodeOnRemoval true and syncCatalog.enabled false and Release.IsUpgrade true" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    . | tee /dev/stderr |
    yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# image

@test "syncCatalogCleanupJob/Upgrade: image defaults to global.imageK8S" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'global.imageK8S=bar' \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    . | tee /dev/stderr |
    yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "syncCatalogCleanupJob/Upgrade: image can be overridden with server.image" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'global.imageK8S=foo' \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'syncCatalog.image=bar' \
    --is-upgrade \
    . | tee /dev/stderr |
    yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "syncCatalogCleanupJob/Upgrade: consul env defaults" {
  cd $(chart_dir)
  local env=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_ADDRESSES").value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-server.default.svc" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_GRPC_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8502" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_HTTP_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8500" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_DATACENTER").value' | tee /dev/stderr)
  [ "${actual}" = "dc1" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_API_TIMEOUT").value' | tee /dev/stderr)
  [ "${actual}" = "5s" ]
}

#--------------------------------------------------------------------
# consulNodeName

@test "syncCatalogCleanupJob/Upgrade: consulNodeName defaults to k8s-sync" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].command | any(contains("-consul-node-name=k8s-sync"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalogCleanupJob/Upgrade: consulNodeName set to empty" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    --set 'syncCatalog.consulNodeName=' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].command | any(contains("-consul-node-name"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalogCleanupJob/Upgrade: can specify consulNodeName" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    --set 'syncCatalog.consulNodeName=aNodeName' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].command | any(contains("-consul-node-name=aNodeName"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# serviceAccount

@test "syncCatalogCleanupJob/Upgrade: serviceAccount set when sync enabled" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    . | tee /dev/stderr |
    yq '.spec.template.spec.serviceAccountName | contains("sync-catalog-cleanup")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# aclSyncToken

@test "syncCatalogCleanupJob/Upgrade: aclSyncToken disabled when secretName is missing" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    --set 'syncCatalog.aclSyncToken.secretKey=bar' \
    . | tee /dev/stderr |
    yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalogCleanupJob/Upgrade: aclSyncToken disabled when secretKey is missing" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    --set 'syncCatalog.aclSyncToken.secretName=foo' \
    . | tee /dev/stderr |
    yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalogCleanupJob/Upgrade: aclSyncToken enabled when secretName and secretKey is provided" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    --set 'syncCatalog.aclSyncToken.secretName=foo' \
    --set 'syncCatalog.aclSyncToken.secretKey=bar' \
    . | tee /dev/stderr |
    yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# affinity

@test "syncCatalogCleanupJob/Upgrade: affinity not set by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    . | tee /dev/stderr |
    yq '.spec.template.spec.affinity == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalogCleanupJob/Upgrade: affinity can be set" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'syncCatalog.affinity=foobar' \
    . | tee /dev/stderr |
    yq '.spec.template.spec | .affinity == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "syncCatalogCleanupJob/Upgrade: nodeSelector is not set by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "syncCatalogCleanupJob/Upgrade: nodeSelector is not set by default with sync enabled" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "syncCatalogCleanupJob/Upgrade: specified nodeSelector" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'syncCatalog.nodeSelector=testing' \
    . | tee /dev/stderr |
    yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# tolerations

@test "syncCatalogCleanupJob/Upgrade: tolerations not set by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.tolerations == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalogCleanupJob/Upgrade: tolerations can be set" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'syncCatalog.tolerations=foobar' \
    . | tee /dev/stderr |
    yq '.spec.template.spec | .tolerations == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "syncCatalogCleanupJob/Upgrade: ACL auth method env vars are set when acls are enabled" {
  cd $(chart_dir)
  local env=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.acls.manageSystemACLs=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_AUTH_METHOD").value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-k8s-component-auth-method" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_DATACENTER").value' | tee /dev/stderr)
  [ "${actual}" = "dc1" ]
  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_META").value' | tee /dev/stderr)
  [ "${actual}" = 'component=sync-catalog,pod=$(NAMESPACE)/$(POD_NAME)' ]
}

@test "syncCatalogCleanupJob/Upgrade: sets global auth method and primary datacenter when federation and acls and namespaces are enabled" {
  cd $(chart_dir)
  local env=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.federation.enabled=true' \
    --set 'global.federation.primaryDatacenter=dc1' \
    --set 'global.datacenter=dc2' \
    --set 'global.enableConsulNamespaces=true' \
    --set 'global.tls.enabled=true' \
    --set 'meshGateway.enabled=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_AUTH_METHOD").value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-k8s-component-auth-method-dc2" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_DATACENTER").value' | tee /dev/stderr)
  [ "${actual}" = "dc1" ]
}

@test "syncCatalogCleanupJob/Upgrade: sets default login partition and acls and partitions are enabled" {
  cd $(chart_dir)
  local env=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.adminPartitions.enabled=true' \
    --set 'global.enableConsulNamespaces=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_PARTITION").value' | tee /dev/stderr)
  [ "${actual}" = "default" ]
}

@test "syncCatalogCleanupJob/Upgrade: sets non-default login partition and acls and partitions are enabled" {
  cd $(chart_dir)
  local env=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.adminPartitions.enabled=true' \
    --set 'global.adminPartitions.name=foo' \
    --set 'global.enableConsulNamespaces=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_PARTITION").value' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "syncCatalogCleanupJob/Upgrade: sets Consul environment variables when global.tls.enabled" {
  cd $(chart_dir)
  local env=$(helm template \
    -s $target \
    --set 'client.enabled=true' \
    --is-upgrade \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    . | tee /dev/stderr |
    yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_HTTP_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8501" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_USE_TLS").value' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_CACERT_FILE").value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "syncCatalogCleanupJob/Upgrade: can overwrite CA secret with the provided one" {
  cd $(chart_dir)
  local ca_cert_volume=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
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

@test "syncCatalogCleanupJob/Upgrade: consul-ca-cert volumeMount is added when TLS is enabled" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalogCleanupJob/Upgrade: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'externalServers.enabled=true' \
    --set 'externalServers.hosts[0]=foo.com' \
    --set 'externalServers.useSystemRoots=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# resources

@test "syncCatalogCleanupJob/Upgrade: default resources" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"50m","memory":"50Mi"},"requests":{"cpu":"50m","memory":"50Mi"}}' ]
}

@test "syncCatalogCleanupJob/Upgrade: can set resources" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'syncCatalog.resources.requests.memory=100Mi' \
    --set 'syncCatalog.resources.requests.cpu=100m' \
    --set 'syncCatalog.resources.limits.memory=200Mi' \
    --set 'syncCatalog.resources.limits.cpu=200m' \
    . | tee /dev/stderr |
    yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"200m","memory":"200Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}' ]
}

#--------------------------------------------------------------------
# extraLabels

@test "syncCatalogCleanupJob/Upgrade: no extra labels defined by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "syncCatalogCleanupJob/Upgrade: can set extra labels" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'syncCatalog.extraLabels.foo=bar' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)

  [ "${actual}" = "bar" ]
}

@test "syncCatalogCleanupJob/Upgrade: extra global labels can be set" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.extraLabels.foo=bar' \
    . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "syncCatalogCleanupJob/Upgrade: multiple extra global labels can be set" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
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
# annotations

@test "syncCatalogCleanupJob/Upgrade: no annotations defined by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject")' |
    tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "syncCatalogCleanupJob/Upgrade: annotations can be set" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'syncCatalog.annotations=foo: bar' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "syncCatalogCleanupJob/Upgrade: metrics annotations can be set" {
  cd $(chart_dir)
  local object=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --is-upgrade \
    --set 'syncCatalog.metrics.enabled=true' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject")' |
    tee /dev/stderr)

  # Annotations to check
  annotations=("prometheus.io/scrape" "prometheus.io/path" "prometheus.io/port")

  # Check each annotation
  for annotation in "${annotations[@]}"; do
    actual=$(echo "$object" | yq -r "has(\"$annotation\")")
    [ "$actual" = "true" ]
  done
}

#--------------------------------------------------------------------
# logLevel

@test "syncCatalogCleanupJob/Upgrade: logLevel info by default from global" {
  cd $(chart_dir)
  local cmd=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalogCleanupJob/Upgrade: logLevel can be overridden" {
  cd $(chart_dir)
  local cmd=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.logLevel=debug' \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault

@test "syncCatalogCleanupJob/Upgrade: configures server CA to come from vault when vault is enabled" {
  cd $(chart_dir)
  local object=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    . | tee /dev/stderr |
    yq -r '.spec.template' | tee /dev/stderr)

  # Check annotations
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-init-first"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "carole" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
  local actual
  actual=$(echo $object | jq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)
  [ "${actual}" = $'{{- with secret \"foo\" -}}\n{{- .Data.certificate -}}\n{{- end -}}' ]

  actual=$(echo $object | jq -r '.spec.volumes[] | select( .name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  actual=$(echo $object | jq -r '.spec.containers[0].volumeMounts[] | select( .name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "syncCatalogCleanupJob/Upgrade: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd $(chart_dir)
  local cmd=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=bar' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.secretsBackend.vault.vaultNamespace=vns' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.tls.enableAutoEncrypt=true' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
    yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "vns" ]
}

@test "syncCatalogCleanupJob/Upgrade: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set without vaultNamespace annotation" {
  cd $(chart_dir)
  local cmd=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=bar' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.secretsBackend.vault.vaultNamespace=vns' \
    --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/agent-extra-secret: bar' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.tls.enableAutoEncrypt=true' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
    yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "vns" ]
}

@test "syncCatalogCleanupJob/Upgrade: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd $(chart_dir)
  local cmd=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=bar' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.secretsBackend.vault.vaultNamespace=vns' \
    --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/namespace: bar' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.tls.enableAutoEncrypt=true' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
    yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "bar" ]
}

@test "syncCatalogCleanupJob/Upgrade: vault CA is not configured by default" {
  cd $(chart_dir)
  local object=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    . | tee /dev/stderr |
    yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "syncCatalogCleanupJob/Upgrade: vault CA is not configured when secretName is set but secretKey is not" {
  cd $(chart_dir)
  local object=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    . | tee /dev/stderr |
    yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "syncCatalogCleanupJob/Upgrade: vault CA is not configured when secretKey is set but secretName is not" {
  cd $(chart_dir)
  local object=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
    yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "syncCatalogCleanupJob/Upgrade: vault CA is configured when both secretName and secretKey are set" {
  cd $(chart_dir)
  local object=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
    yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

#--------------------------------------------------------------------
# Vault agent annotations

@test "syncCatalogCleanupJob/Upgrade: no vault agent annotations defined by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."vault.hashicorp.com/agent-inject") |
      del(."vault.hashicorp.com/role")' |
    tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "syncCatalogCleanupJob/Upgrade: vault agent annotations can be set" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.enabled=false' \
    --is-upgrade \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.consulCARole=carole' \
    --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
    . | tee /dev/stderr |
    yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
