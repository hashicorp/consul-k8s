#!/usr/bin/env bats

load _helpers

@test "dnsProxy/Service: disabled by default" {
  cd `chart_dir`
      assert_empty helm template \
        -s templates/dns-proxy-service.yaml  \
        .
}

@test "dnsProxy/Service: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-service.yaml  \
      --set 'global.enabled=false' \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/Service: disable with connectInject.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-proxy-service.yaml  \
      --set 'dns.proxy.enabled=false' \
      .
}

@test "dnsProxy/Service: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-proxy-service.yaml  \
      --set 'dns.proxy.enabled=-' \
      --set 'global.enabled=false' \
      .
}
