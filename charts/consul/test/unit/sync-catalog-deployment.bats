#!/usr/bin/env bats

load _helpers

@test "syncCatalog/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-deployment.yaml  \
      .
}

@test "syncCatalog/Deployment: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: disable with syncCatalog.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=false' \
      .
}

@test "syncCatalog/Deployment: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'global.enabled=false' \
      .
}

#--------------------------------------------------------------------
# image

@test "syncCatalog/Deployment: image defaults to global.imageK8S" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'global.imageK8S=bar' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "syncCatalog/Deployment: image can be overridden with server.image" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'global.imageK8S=foo' \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.image=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "syncCatalog/Deployment: consul env defaults" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/sync-catalog-deployment.yaml \
      --set 'syncCatalog.enabled=true' \
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
# default sync

@test "syncCatalog/Deployment: default sync is true by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | any(contains("-k8s-default-sync=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: default sync can be turned off" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.default=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | any(contains("-k8s-default-sync=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# toConsul and toK8S

@test "syncCatalog/Deployment: bidirectional by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-to-consul"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-to-k8s"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: to-k8s only" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.toConsul=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-to-consul=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.toConsul=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-to-k8s"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: to-consul only" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.toK8S=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-to-k8s=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.toK8S=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-to-consul"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# k8sPrefix

@test "syncCatalog/Deployment: no k8sPrefix by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-k8s-service-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: can specify k8sPrefix" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.k8sPrefix=foo-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-k8s-service-prefix=\"foo-\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# consulPrefix

@test "syncCatalog/Deployment: no consulPrefix by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-service-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: can specify consulPrefix" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.consulPrefix=foo-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-service-prefix=\"foo-\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# k8sTag

@test "syncCatalog/Deployment: no k8sTag flag by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-k8s-tag"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: can specify k8sTag" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.k8sTag=clusterB' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-k8s-tag=clusterB"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# consulNodeName

@test "syncCatalog/Deployment: consulNodeName defaults to k8s-sync" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-node-name=k8s-sync"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: consulNodeName set to empty" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.consulNodeName=' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-node-name"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: can specify consulNodeName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.consulNodeName=aNodeName' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-node-name=aNodeName"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# serviceAccount

@test "syncCatalog/Deployment: serviceAccount set when sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.serviceAccountName | contains("sync-catalog")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodePortSyncType

@test "syncCatalog/Deployment: nodePortSyncType defaults to ExternalFirst" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-node-port-sync-type=ExternalFirst"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: can set nodePortSyncType to InternalOnly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.nodePortSyncType=InternalOnly' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-node-port-sync-type=InternalOnly"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: can set nodePortSyncType to ExternalOnly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.nodePortSyncType=ExternalOnly' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-node-port-sync-type=ExternalOnly"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# aclSyncToken

@test "syncCatalog/Deployment: aclSyncToken disabled when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.aclSyncToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: aclSyncToken disabled when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.aclSyncToken.secretName=foo' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: aclSyncToken enabled when secretName and secretKey is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.aclSyncToken.secretName=foo' \
      --set 'syncCatalog.aclSyncToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# sync ingress

@test "syncCatalog/Deployment: enable ingress sync flag not passed when disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.ingress.enabled=false' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-ingress=true"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
@test "syncCatalog/Deployment: enable ingress sync flag passed when enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.ingress.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-ingress=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: enable loadbalancer IP sync flag not passed when  syncIngress disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.ingress.enabled=false' \
      --set 'syncCatalog.ingress.loadBalancerIPs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-loadBalancer-ips=true"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: enable loadbalancer IP sync flag passed when enabled with ingress sync" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.ingress.enabled=true' \
      --set 'syncCatalog.ingress.loadBalancerIPs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-loadBalancer-ips=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# syncLoadBalancerEndpoints

@test "syncCatalog/Deployment: enable LB endpoints sync flag not passed when disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-sync-lb-services-endpoints=true"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
@test "syncCatalog/Deployment: enable LB endpoints sync flag passed when enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.syncLoadBalancerEndpoints=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-sync-lb-services-endpoints=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# affinity

@test "syncCatalog/Deployment: affinity not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.affinity == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: affinity can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.affinity=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .affinity == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "syncCatalog/Deployment: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "syncCatalog/Deployment: nodeSelector is not set by default with sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "syncCatalog/Deployment: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# tolerations

@test "syncCatalog/Deployment: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.tolerations=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .tolerations == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "syncCatalog/Deployment: ACL auth method env vars are set when acls are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/sync-catalog-deployment.yaml \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: sets global auth method and primary datacenter when federation and acls and namespaces are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/sync-catalog-deployment.yaml \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: sets default login partition and acls and partitions are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/sync-catalog-deployment.yaml \
      --set 'syncCatalog.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_PARTITION").value' | tee /dev/stderr)
  [ "${actual}" = "default" ]
}

@test "syncCatalog/Deployment: sets non-default login partition and acls and partitions are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/sync-catalog-deployment.yaml \
      --set 'syncCatalog.enabled=true' \
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
# addK8SNamespaceSuffix

@test "syncCatalog/Deployment: k8s namespace suffix enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-add-k8s-namespace-suffix"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: can set addK8SNamespaceSuffix to false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.addK8SNamespaceSuffix=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-add-k8s-namespace-suffix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "syncCatalog/Deployment: sets Consul environment variables when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: consul-ca-cert volumeMount is added when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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
# k8sAllowNamespaces & k8sDenyNamespaces

@test "syncCatalog/Deployment: default is allow `*`, deny kube-system and kube-public" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'map(select(test("allow-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'any(contains("allow-k8s-namespace=\"*\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("deny-k8s-namespace=\"kube-system\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("deny-k8s-namespace=\"kube-public\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: can set allow and deny namespaces {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.k8sAllowNamespaces[0]=allowNamespace' \
      --set 'syncCatalog.k8sDenyNamespaces[0]=denyNamespace' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'map(select(test("allow-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'map(select(test("deny-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'any(contains("allow-k8s-namespace=\"allowNamespace\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("deny-k8s-namespace=\"denyNamespace\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# namespaces

@test "syncCatalog/Deployment: namespace options disabled by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: namespace options set with .global.enableConsulNamespaces=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: mirroring options set with .syncCatalog.consulNamespaces.mirroringK8S=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'syncCatalog.consulNamespaces.mirroringK8S=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: prefix can be set with .syncCatalog.consulNamespaces.mirroringK8SPrefix" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'syncCatalog.consulNamespaces.mirroringK8S=true' \
      --set 'syncCatalog.consulNamespaces.mirroringK8SPrefix=k8s-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix=k8s-"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# namespaces + global.acls.manageSystemACLs

@test "syncCatalog/Deployment: cross namespace policy is not added when global.acls.manageSystemACLs=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml \
      --set 'syncCatalog.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-cross-namespace-acl-policy"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/Deployment: cross namespace policy is added when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml \
      --set 'syncCatalog.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-cross-namespace-acl-policy"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# resources

@test "syncCatalog/Deployment: default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"50m","memory":"50Mi"},"requests":{"cpu":"50m","memory":"50Mi"}}' ]
}

@test "syncCatalog/Deployment: can set resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.resources.requests.memory=100Mi' \
      --set 'syncCatalog.resources.requests.cpu=100m' \
      --set 'syncCatalog.resources.limits.memory=200Mi' \
      --set 'syncCatalog.resources.limits.cpu=200m' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"200m","memory":"200Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}' ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "syncCatalog/Deployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "syncCatalog/Deployment: can set a priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.priorityClassName=name' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "name" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "syncCatalog/Deployment: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "syncCatalog/Deployment: can set extra labels" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.extraLabels.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)

  [ "${actual}" = "bar" ]
}

@test "syncCatalog/Deployment: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "syncCatalog/Deployment: multiple extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "syncCatalog/Deployment: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# logLevel

@test "syncCatalog/Deployment: logLevel info by default from global" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/Deployment: logLevel can be overridden" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.logLevel=debug' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault

@test "syncCatalog/Deployment: configures server CA to come from vault when vault is enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set without vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/sync-catalog-deployment.yaml  \
    --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/sync-catalog-deployment.yaml  \
    --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/sync-catalog-deployment.yaml  \
    --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/sync-catalog-deployment.yaml  \
    --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
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

# consulDestinationNamespace reserved name

@test "syncCatalog/Deployment: fails when consulDestinationNamespace=system" {
  reservedNameTest "system"
}

@test "syncCatalog/Deployment: fails when consulDestinationNamespace=universal" {
  reservedNameTest "universal"
}

@test "syncCatalog/Deployment: fails when consulDestinationNamespace=operator" {
  reservedNameTest "operator"
}

@test "syncCatalog/Deployment: fails when consulDestinationNamespace=root" {
  reservedNameTest "root"
}

# reservedNameTest is a helper function that tests if certain Consul destination
# namespace names fail because the name is reserved.
reservedNameTest() {
  cd `chart_dir`
  local -r name="$1"
		run helm template \
				-s templates/sync-catalog-deployment.yaml  \
				--set 'syncCatalog.enabled=true' \
				--set "syncCatalog.consulNamespaces.consulDestinationNamespace=$name" .

		[ "$status" -eq 1 ]
		[[ "$output" =~ "The name $name set for key syncCatalog.consulNamespaces.consulDestinationNamespace is reserved by Consul for future use" ]]
}

#--------------------------------------------------------------------
# global.cloud

@test "syncCatalog/Deployment: fails when global.cloud.enabled is true and global.cloud.clientId.secretName is not set but global.cloud.clientSecret.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientSecret.secretName=client-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=client-resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=client-resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When either global.cloud.resourceId.secretName or global.cloud.resourceId.secretKey is defined, both must be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.authUrl.secretName=auth-url-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.authUrl.secretName or global.cloud.authUrl.secretKey is defined, both must be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.authUrl.secretKey=auth-url-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.authUrl.secretName or global.cloud.authUrl.secretKey is defined, both must be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.apiHost.secretName=auth-url-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.apiHost.secretName or global.cloud.apiHost.secretKey is defined, both must be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.apiHost.secretKey=auth-url-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.apiHost.secretName or global.cloud.apiHost.secretKey is defined, both must be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.scadaAddress.secretName=scada-address-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.scadaAddress.secretName or global.cloud.scadaAddress.secretKey is defined, both must be set." ]]
}

@test "syncCatalog/Deployment: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/sync-catalog-deployment.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.scadaAddress.secretKey=scada-address-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.scadaAddress.secretName or global.cloud.scadaAddress.secretKey is defined, both must be set." ]]
}
