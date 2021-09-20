#!/usr/bin/env bats

load _helpers

@test "autogenEncryption/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogen-job.yaml  \
      .
}

@test "autogenEncryption/Job: enabled with global.gossipEncryption.autoGenerate=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogen-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "autogenEncryption/Job: disabled when global.gossipEncryption.autoGenerate=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/gossip-encryption-autogen-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=false' \
      .
}

@test "autogenEncryption/Job: fails if global.gossipEncryption.autoGenerate=true and global.gossipEncryption.secretName and global.gossipEncryption.secretKey are set" {
  cd `chart_dir`
  run helm template \
      -s templates/gossip-encryption-autogen-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.gossipEncryption.secretName=name' \
      --set 'global.gossipEncryption.secretKey=key' \
      . 
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.gossipEncryption.autoGenerate is true, global.gossipEncryption.secretName and global.gossipEncryption.secretKey must not be set." ]]
}

@test "autogenEncryption/Job: fails if global.gossipEncryption.autoGenerate=true and global.gossipEncryption.secretName are set" {
  cd `chart_dir`
  run helm template \
      -s templates/gossip-encryption-autogen-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.gossipEncryption.secretName=name' \
      . 
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.gossipEncryption.autoGenerate is true, global.gossipEncryption.secretName and global.gossipEncryption.secretKey must not be set." ]]
}

@test "autogenEncryption/Job: fails if global.gossipEncryption.autoGenerate=true and global.gossipEncryption.secretKey are set" {
  cd `chart_dir`
  run helm template \
      -s templates/gossip-encryption-autogen-job.yaml  \
      --set 'global.gossipEncryption.autoGenerate=true' \
      --set 'global.gossipEncryption.secretKey=key' \
      . 
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.gossipEncryption.autoGenerate is true, global.gossipEncryption.secretName and global.gossipEncryption.secretKey must not be set." ]]
}


@test "autogenEncryption/Job: secretName and secretKey are generated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/gossip-encryption-autogen-job.yaml \
      --set 'global.gossipEncryption.autoGenerate=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("secretName=RELEASE-NAME-consul-gossip-encryption-key\nsecretKey=key"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

