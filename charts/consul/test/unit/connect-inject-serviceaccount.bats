#!/usr/bin/env bats

load _helpers

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "connectInject/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/ServiceAccount: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ServiceAccount: annotations when enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
      --set "connectInject.serviceAccount.annotations=foo: bar" \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
