#!/usr/bin/env bats

load _helpers

@test "dnsProxy/Deployment: disabled by default" {
  cd `chart_dir`
      assert_empty helm template \
        -s templates/dns-proxy-deployment.yaml  \
        .
}

@test "dnsProxy/Deployment: enable with global.enabled false, client.enabled true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/Deployment: disable with dns.proxy.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=false' \
      .
}

@test "dnsProxy/Deployment: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=-' \
      --set 'global.enabled=false' \
      .
}

#--------------------------------------------------------------------
# flags

@test "dnsProxy/Deployment: default dns-proxy mode flag" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/dns-proxy-deployment.yaml \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-mode=dns-proxy"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# consul and consul-dataplane images

@test "dnsProxy/Deployment: container image is global default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.imageConsulDataplane=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "\"foo\"" ]
}


#--------------------------------------------------------------------
# nodeSelector

@test "dnsProxy/Deployment: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}


#--------------------------------------------------------------------
# authMethod

@test "dnsProxy/Deployment: -login-auth-method is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-login-auth-method="))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "dnsProxy/Deployment: -login-auth-method is set when global.acls.manageSystemACLs is true -login-auth-method" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("-login-auth-method=release-name-consul-k8s-component-auth-method"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/Deployment: -login-auth-method is set when global.acls.manageSystemACLs is true -credential-type" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("-credential-type=login"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/Deployment: -login-auth-method is set when global.acls.manageSystemACLs is true login-bearer-token-path" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("-login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "dnsProxy/Deployment: Adds consul-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "dnsProxy/Deployment: Adds consul-ca-cert volumeMount when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "dnsProxy/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
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

@test "dnsProxy/Deployment: consul env vars when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml \
      --set 'dns.proxy.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("-ca-certs=/consul/tls/ca/tls.crt"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}


#--------------------------------------------------------------------
# partitions

@test "dnsProxy/Deployment: partitions options disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("service-partitions"))' | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "dnsProxy/Deployment: partitions set with .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("service-partition=default"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "dnsProxy/Deployment: partitions set with .global.adminPartitions.enabled=true and name set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=ap1' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("service-partition=ap1"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# acl tokens


#--------------------------------------------------------------------
# global.acls.manageSystemACLs
@test "dnsProxy/Deployment: sets global auth method and primary datacenter when federation and acls" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml \
      --set 'dns.proxy.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'global.datacenter=dc2' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args | any(contains("-login-datacenter=dc1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/Deployment: sets default login partition and acls and partitions are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml \
      --set 'dns.proxy.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
       yq '.spec.template.spec.containers[0].args | any(contains("-login-partition=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/Deployment: sets non-default login partition and acls and partitions are enabled" {
    cd `chart_dir`
    local actual=$(helm template \
        -s templates/dns-proxy-deployment.yaml \
        --set 'dns.proxy.enabled=true' \
        --set 'global.acls.manageSystemACLs=true' \
        --set 'global.adminPartitions.enabled=true' \
        --set 'global.adminPartitions.name=foo' \
        --set 'global.enableConsulNamespaces=true' \
        . | tee /dev/stderr |
         yq '.spec.template.spec.containers[0].args | any(contains("-login-partition=foo"))' | tee /dev/stderr)
    [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "dnsProxy/Deployment: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "dnsProxy/Deployment: can set extra global labels" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "dnsProxy/Deployment: can set multiple extra global labels" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
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

@test "dnsProxy/Deployment: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "dnsProxy/Deployment:  default annotations connect-inject" {
  cd `chart_dir`
      local actual=$(helm template \
          -s templates/dns-proxy-deployment.yaml \
          --set 'dns.proxy.enabled=true' \
          . | tee /dev/stderr |
           yq '.spec.template.metadata.annotations["consul.hashicorp.com/connect-inject"]' | tee /dev/stderr)
      [ "${actual}" = "\"false\"" ]
}

@test "dnsProxy/Deployment:  default annotations mesh-inject" {
  cd `chart_dir`
      local actual=$(helm template \
          -s templates/dns-proxy-deployment.yaml \
          --set 'dns.proxy.enabled=true' \
          . | tee /dev/stderr |
           yq '.spec.template.metadata.annotations["consul.hashicorp.com/mesh-inject"]' | tee /dev/stderr)
      [ "${actual}" = "\"false\"" ]
}

#--------------------------------------------------------------------
# logLevel

@test "dnsProxy/Deployment: logLevel info by default from global" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/Deployment: logLevel can be overridden" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'dns.proxy.logLevel=debug' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].args' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}



#--------------------------------------------------------------------
# replicas

@test "dnsProxy/Deployment: replicas defaults to 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "1" ]
}

@test "dnsProxy/Deployment: replicas can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-deployment.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'dns.proxy.replicas=3' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "3" ]
}