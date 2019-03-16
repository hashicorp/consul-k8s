#!/usr/bin/env bats

load _helpers

@test "global/gossipEncryption: disabled in server StatefulSet by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "global/gossipEncryption: disabled in server StatefulSet when servers are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enabled=false' \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretName=foo' \
      --set 'global.gossipEncryption.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "global/gossipEncryption: disabled in server StatefulSet when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "global/gossipEncryption: disabled in server StatefulSet when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "global/gossipEncryption: environment variable present in server StatefulSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "global/gossipEncryption: CLI option present in server StatefulSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.enabled=true' \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | join(" ") | contains("encrypt")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
