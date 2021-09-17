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

@test "autogenEncryption/Job: secretName and secretKey set by user are respected" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/autogen-encryption-job.yaml \
      --set 'global.gossipEncryption.autogenerate=true' \
      --set 'global.gossipEncryption.secretName=userName' \
      --set 'global.gossipEncryption.secretKey=userKey' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("secretName=userName\nsecretKey=userKey"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "autogenEncryption/Job: secretKey set by user is respected while secretName is generated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/autogen-encryption-job.yaml \
      --set 'global.gossipEncryption.autogenerate=true' \
      --set 'global.gossipEncryption.secretKey=userKey' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("secretName=RELEASE-NAME-consul-gossip-encryption-key\nsecretKey=userKey"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "autogenEncryption/Job: secretName set by user is respected while secretKey is generated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/autogen-encryption-job.yaml \
      --set 'global.gossipEncryption.autogenerate=true' \
      --set 'global.gossipEncryption.secretName=userName' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("secretName=userName\nsecretKey=key"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "autogenEncryption/Job: secretName and secretKey are generated if not provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/autogen-encryption-job.yaml \
      --set 'global.gossipEncryption.autogenerate=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("secretName=RELEASE-NAME-consul-gossip-encryption-key\nsecretKey=key"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

