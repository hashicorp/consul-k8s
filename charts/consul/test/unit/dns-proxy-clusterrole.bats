#!/usr/bin/env bats

load _helpers

@test "dnsProxy/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
    -s templates/dns-proxy-clusterrole.yaml  \
    .
}
@test "dnsProxy/ClusterRole: enabled with global.rbac.create false" {
  cd `chart_dir`
    assert_empty helm template \
        -s templates/dns-proxy-clusterrole.yaml \
        --set 'dns.proxy.enabled=true' \
        --set 'global.rbac.create=false'  \
        .
}
@test "dnsProxy/ClusterRole: dns-proxy with global.enabled false" {
  cd `chart_dir`
    local actual=$(helm template \
        -s templates/dns-proxy-clusterrole.yaml  \
        --set 'global.enabled=false' \
        --set 'dns.proxy.enabled=true' \
        . | tee /dev/stderr |
        yq -s 'length > 0' | tee /dev/stderr)
    [ "${actual}" = "true" ]
}

@test "dnsProxy/ClusterRole: disabled with dns.proxy.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-proxy-clusterrole.yaml  \
      --set 'dns.proxy.enabled=false' \
      .
}

##--------------------------------------------------------------------
## rules
#
@test "dnsProxy/ClusterRole: sets get, list, and watch access to endpoints, services, namespaces and nodes in all api groups" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/dns-proxy-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules | length' | tee /dev/stderr)
      [ "${object}" == 0 ]
}


@test "dnsProxy/ClusterRole: sets get access to serviceaccounts and secrets when manageSystemACLSis true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/dns-proxy-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'dns.proxy.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0]' | tee /dev/stderr)

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
@test "dnsProxy/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-clusterrole.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "dnsProxy/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-clusterrole.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
