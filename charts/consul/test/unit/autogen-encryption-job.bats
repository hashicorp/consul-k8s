#!/usr/bin/env bats

load _helpers

@test "autogenEncryption/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/autogen-encryption-job.yaml  \
      .
}

@test "autogenEncryption/Job: enabled with global.gossipEncryption.autogenerate=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/autogen-encryption-job.yaml  \
      --set 'global.gossipEncryption.autogenerate=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "autogenEncryption/Job: disabled when global.gossipEncryption.autogenerate=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/autogen-encryption-job.yaml  \
      --set 'global.gossipEncryption.autogenerate=false' \
      .
}

# TODO test when user sets secretKey
# TODO test when user sets secretName
# TODO test when user sets secretKey and secretName
# TODO test when user does not set either
