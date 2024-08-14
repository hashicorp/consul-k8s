#!/usr/bin/env bats

load _helpers

@test "client/TmpExtraConfigMap: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-tmp-extra-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/TmpExtraConfigMap: disable with client.enabled false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-tmp-extra-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'client.enabled=false' \
      .
}

@test "client/TmpExtraConfigMap: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-tmp-extra-config-configmap.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "client/TmpExtraConfigMap: extraConfig is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-tmp-extra-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq '.data["extra-from-values.json"] | match("world") | length > 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
