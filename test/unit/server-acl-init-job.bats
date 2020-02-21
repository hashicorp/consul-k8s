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

@test "serverACLInit/Job: enabled with global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: disabled with server=false and global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: enabled with client=false global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: disabled when server.updatePartition > 0" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.updatePartition=1' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: does not set -create-client-token=false when client is enabled (the default)" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2] | contains("-create-client-token=false")' |
      tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: sets -create-client-token=false when client is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command[2] | contains("-create-client-token=false")' |
      tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# dns

@test "serverACLInit/Job: dns acl option enabled with .dns.enabled=-" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: dns acl option enabled with .dns.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: dns acl option disabled with .dns.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'dns.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("allow-dns"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# aclBindingRuleSelector/global.bootstrapACLs

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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-enterprise-license-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: ent license acl option disabled missing server.enterpriseLicense.secretKey" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-snapshot-agent-token"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/Job: snapshot agent acl option enabled with .client.snapshotAgent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'client.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-create-snapshot-agent-token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/Job: mesh gateway acl option enabled with .meshGateway.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | any(contains("-use-https"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | any(contains("-consul-ca-cert=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

    actual=$(echo $command | jq -r '. | any(contains("-consul-tls-server-name=server.dc1.consul"))' | tee /dev/stderr)
    [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# namespaces

@test "serverACLInit/Job: namespace options disabled by default" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-job.yaml  \
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
      --set 'global.bootstrapACLs=true' \
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
