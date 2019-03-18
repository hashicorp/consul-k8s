#!/usr/bin/env bats

load _helpers

@test "global/gossipEncryption: disabled in client DaemonSet by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-daemonset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "global/gossipEncryption: disabled in client DaemonSet when servers are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-daemonset.yaml  \
      --set 'server.enabled=false' \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretName=foo' \
      --set 'global.gossipEncryption.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "global/gossipEncryption: disabled in client DaemonSet when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-daemonset.yaml  \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "global/gossipEncryption: disabled in client DaemonSet when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-daemonset.yaml  \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "global/gossipEncryption: environment variable present in client DaemonSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-daemonset.yaml  \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "global/gossipEncryption: CLI option present in client DaemonSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-daemonset.yaml  \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | join(" ") | contains("encrypt")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
