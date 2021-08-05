#!/usr/bin/env bats

load _helpers

@test "client/ServiceAccount: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ServiceAccount: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "client/ServiceAccount: disabled with client disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-serviceaccount.yaml  \
      --set 'client.enabled=false' \
      .
}

@test "client/ServiceAccount: enabled with client enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-serviceaccount.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ServiceAccount: enabled with client enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "client/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-serviceaccount.yaml  \
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
# client.serviceAccount.annotations

@test "client/ServiceAccount: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ServiceAccount: annotations when enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-serviceaccount.yaml \
      --set "client.serviceAccount.annotations=foo: bar" \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
