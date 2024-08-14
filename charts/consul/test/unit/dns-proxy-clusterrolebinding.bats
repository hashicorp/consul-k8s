#!/usr/bin/env bats

load _helpers

@test "dnsProxy/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
 assert_empty helm template \
      -s templates/dns-proxy-clusterrolebinding.yaml  \
      .
}

@test "dnsProxy/ClusterRoleBinding: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-clusterrolebinding.yaml  \
      --set 'global.enabled=false' \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/ClusterRoleBinding: disabled with connectInject.enabled false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-proxy-clusterrolebinding.yaml  \
      --set 'dns.proxy.enabled=false' \
      .
}
