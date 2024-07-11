#!/usr/bin/env bats

load _helpers

@test "dnsProxy/ServiceAccount: disabled by default" {
    cd `chart_dir`
    assert_empty helm template \
      -s templates/dns-proxy-serviceaccount.yaml  \
      .
}

@test "dnsProxy/ServiceAccount: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "dnsProxy/ServiceAccount: disabled with connectInject.enabled false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/dns-proxy-serviceaccount.yaml  \
      --set 'dns.proxy.enabled=false' \
      .
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "dnsProxy/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/dns-proxy-serviceaccount.yaml  \
      --set 'dns.proxy.enabled=true' \
      --set 'global.imagePullSecrets[0].name=my-secret' \
      --set 'global.imagePullSecrets[1].name=my-secret2' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}

#--------------------------------------------------------------------
# connectInject.serviceAccount.annotations

@test "dnsProxy/ServiceAccount: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/dns-proxy-serviceaccount.yaml  \
      --set 'dns.proxy.enabled=true' \
      . | tee /dev/stderr |
      yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
