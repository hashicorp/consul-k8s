#!/usr/bin/env bats

load _helpers

@test "server/StatefulSet: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: disable with server.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# image

@test "server/StatefulSet: image defaults to global.image" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.image=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

@test "server/StatefulSet: image can be overridden with server.image" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.image=foo' \
      --set 'server.image=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# resources

@test "server/StatefulSet: no resources defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: resources can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.resources=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

#--------------------------------------------------------------------
# updateStrategy (derived from updatePartition)

@test "server/StatefulSet: no updateStrategy when not updating" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.updateStrategy' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: updateStrategy during update" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.updatePartition=2' \
      . | tee /dev/stderr |
      yq -r '.spec.updateStrategy.type' | tee /dev/stderr)
  [ "${actual}" = "RollingUpdate" ]

  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.updatePartition=2' \
      . | tee /dev/stderr |
      yq -r '.spec.updateStrategy.rollingUpdate.partition' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

#--------------------------------------------------------------------
# storageClass

@test "server/StatefulSet: no storageClass on claim by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.volumeClaimTemplates[0].spec.storageClassName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: can set storageClass" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.storageClass=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.volumeClaimTemplates[0].spec.storageClassName' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

#--------------------------------------------------------------------
# extraVolumes

@test "server/StatefulSet: adds extra volume" {
  cd `chart_dir`

  # Test that it defines it
  local object=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=configMap' \
      --set 'server.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.configMap.name' | tee /dev/stderr)
  [ "${actual}" = "foo" ]

  local actual=$(echo $object |
      yq -r '.configMap.secretName' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  # Test that it mounts it
  local object=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=configMap' \
      --set 'server.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/userconfig/foo" ]

  # Doesn't load it
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=configMap' \
      --set 'server.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | map(select(test("userconfig"))) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/StatefulSet: adds extra secret volume" {
  cd `chart_dir`

  # Test that it defines it
  local object=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=secret' \
      --set 'server.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.secret.name' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo $object |
      yq -r '.secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo" ]

  # Test that it mounts it
  local object=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=configMap' \
      --set 'server.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "userconfig-foo")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/userconfig/foo" ]

  # Doesn't load it
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=configMap' \
      --set 'server.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | map(select(test("userconfig"))) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/StatefulSet: adds loadable volume" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=configMap' \
      --set 'server.extraVolumes[0].name=foo' \
      --set 'server.extraVolumes[0].load=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | map(select(test("/consul/userconfig/foo"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# affinity

@test "server/StatefulSet: affinity not set with server.affinity=null" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.affinity=null' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .affinity? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: affinity set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.affinity | .podAntiAffinity? != null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "server/StatefulSet: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml \
      --set 'server.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "server/StatefulSet: priorityClassName is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: specified priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml \
      --set 'server.priorityClassName=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# annotations

@test "server/StatefulSet: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations | del(."consul.hashicorp.com/connect-inject")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "server/StatefulSet: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# tolerations

@test "server/StatefulSet: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .tolerations? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.tolerations=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# gossip encryption

@test "server/StatefulSet: gossip encryption disabled in server StatefulSet by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "server/StatefulSet: gossip encryption disabled in server StatefulSet when servers are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enabled=false' \
      --set 'global.gossipEncryption.secretName=foo' \
      --set 'global.gossipEncryption.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: gossip encryption disabled in server StatefulSet when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "server/StatefulSet: gossip encryption disabled in server StatefulSet when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "server/StatefulSet: gossip environment variable present in server StatefulSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: encrypt CLI option not present in server StatefulSet when encryption disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | join(" ") | contains("encrypt")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: encrypt CLI option present in server StatefulSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | join(" ") | contains("encrypt")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# extraEnvironmentVariables

@test "server/StatefulSet: custom environment variables" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.extraEnvironmentVars.custom_proxy=fakeproxy' \
      --set 'server.extraEnvironmentVars.no_proxy=custom_no_proxy' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.[2].name' | tee /dev/stderr)
  [ "${actual}" = "custom_proxy" ]

  local actual=$(echo $object |
      yq -r '.[2].value' | tee /dev/stderr)
  [ "${actual}" = "fakeproxy" ]

  local actual=$(echo $object |
      yq -r '.[3].name' | tee /dev/stderr)
  [ "${actual}" = "no_proxy" ]

  local actual=$(echo $object |
      yq -r '.[3].value' | tee /dev/stderr)
  [ "${actual}" = "custom_no_proxy" ]
}
