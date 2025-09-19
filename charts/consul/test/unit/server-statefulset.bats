#!/usr/bin/env bats

load _helpers

@test "server/StatefulSet: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: disable with server.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=false' \
      .
}

@test "server/StatefulSet: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.enabled=false' \
      .
}

#--------------------------------------------------------------------
# server.replicas and server.bootstrapExpect

@test "server/StatefulSet: errors if bootstrapExpect < replicas" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.bootstrapExpect=1' \
      --set 'server.replicas=3' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "server.bootstrapExpect cannot be less than server.replicas" ]]
}

#--------------------------------------------------------------------
# admin-partitions

@test "server/StatefulSet: federation and admin partitions cannot be enabled together" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.federation.enabled=true' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.federation.enabled is true, global.adminPartitions.enabled must be false because they are mutually exclusive" ]]
}

#--------------------------------------------------------------------
# image

@test "server/StatefulSet: image defaults to global.image" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.image=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

@test "server/StatefulSet: image can be overridden with server.image" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.image=foo' \
      --set 'server.image=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# resources

@test "server/StatefulSet: resources defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"requests":{"cpu":"100m","memory":"200Mi"},"limits":{"memory":"200Mi"}}' ]
}

@test "server/StatefulSet: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.resources.limits.cpu=4' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.limits.cpu' | tee /dev/stderr)
  [ "${actual}" = 4 ]
}

# Test support for the deprecated method of setting a YAML string.
@test "server/StatefulSet: resources can be overridden with string" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.resources.limits.cpu="2000m"' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.limits.cpu' | tee /dev/stderr)
  [ "${actual}" = "2000m" ]
}

#--------------------------------------------------------------------
# updateStrategy (derived from updatePartition)

@test "server/StatefulSet: no updateStrategy when not updating" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.updateStrategy' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: updateStrategy during update" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.updatePartition=2' \
      . | tee /dev/stderr |
      yq -r '.spec.updateStrategy.type' | tee /dev/stderr)
  [ "${actual}" = "RollingUpdate" ]

  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.updatePartition=2' \
      . | tee /dev/stderr |
      yq -r '.spec.updateStrategy.rollingUpdate.partition' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

#--------------------------------------------------------------------
# volumeClaim name

@test "server/StatefulSet: no truncation for namespace <= 58 chars" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.volumeClaimTemplates[0].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "data-default" ]
}

@test "server/StatefulSet: truncation for namespace > 58 chars" {
  cd `chart_dir`
  local actual=$(helm template \
      -n really-really-really-really-really-really-really-long-namespace \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.volumeClaimTemplates[0].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "data-really-really-really-really-really-really-really-long-name" ]
}

#--------------------------------------------------------------------
# storageClass

@test "server/StatefulSet: no storageClass on claim by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.volumeClaimTemplates[0].spec.storageClassName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: can set storageClass" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.storageClass=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.volumeClaimTemplates[0].spec.storageClassName' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

#--------------------------------------------------------------------
# persistentVolumeClaimRetentionPolicy

@test "server/StatefulSet: persistentVolumeClaimRetentionPolicy not set by default when kubernetes < 1.23" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --kube-version "1.22" \
      . | tee /dev/stderr |
      yq -r '.spec.persistentVolumeClaimRetentionPolicy' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: unset persistentVolumeClaimRetentionPolicy.whenDeleted when kubernetes < 1.23" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --kube-version "1.22" \
      --set 'server.persistentVolumeClaimRetentionPolicy.whenDeleted=Delete' \
      . | tee /dev/stderr |
      yq -r '.spec.persistentVolumeClaimRetentionPolicy.whenDeleted' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: unset persistentVolumeClaimRetentionPolicy.whenScaled when kubernetes < 1.23" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --kube-version "1.22" \
      --set 'server.persistentVolumeClaimRetentionPolicy.whenScaled=Delete' \
      . | tee /dev/stderr |
      yq -r '.spec.persistentVolumeClaimRetentionPolicy.whenScaled' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: persistentVolumeClaimRetentionPolicy not set by default when kubernetes >= 1.23" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --kube-version "1.23" \
      . | tee /dev/stderr |
      yq -r '.spec.persistentVolumeClaimRetentionPolicy' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: can set persistentVolumeClaimRetentionPolicy.whenDeleted when kubernetes >= 1.23" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --kube-version "1.23" \
      --set 'server.persistentVolumeClaimRetentionPolicy.whenDeleted=Delete' \
      . | tee /dev/stderr |
      yq -r '.spec.persistentVolumeClaimRetentionPolicy.whenDeleted' | tee /dev/stderr)
  [ "${actual}" = "Delete" ]
}

@test "server/StatefulSet: can set persistentVolumeClaimRetentionPolicy.whenScaled when kubernetes >= 1.23" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --kube-version "1.23" \
      --set 'server.persistentVolumeClaimRetentionPolicy.whenScaled=Delete' \
      . | tee /dev/stderr |
      yq -r '.spec.persistentVolumeClaimRetentionPolicy.whenScaled' | tee /dev/stderr)
  [ "${actual}" = "Delete" ]
}

#--------------------------------------------------------------------
# serverCert

@test "server/StatefulSet: consul-server-cert uses default cert when serverCert.secretName not set" {
    cd `chart_dir`
    local object=$(helm template \
        -s templates/server-statefulset.yaml \
        --set 'global.tls.enabled=true' \
        --set 'server.serverCert.secretName=null' \
        . | tee /dev/stderr )

    local actual=$(echo "$object" |
        yq -r '.spec.template.spec.volumes[] | select(.name == "consul-server-cert") | .secret.secretName' | tee /dev/stderr)
    [ "${actual}" = "release-name-consul-server-cert" ]
}

@test "server/StatefulSet: consul-server-cert uses serverCert.secretName when serverCert (and caCert) are set" {
    cd `chart_dir`
    local object=$(helm template \
        -s templates/server-statefulset.yaml \
        --set 'global.tls.enabled=true' \
        --set 'global.tls.caCert.secretName=ca-cert' \
        --set 'server.serverCert.secretName=server-cert' \
        . | tee /dev/stderr )

    local actual=$(echo "$object" |
        yq -r '.spec.template.spec.volumes[] | select(.name == "consul-server-cert") | .secret.secretName' | tee /dev/stderr)
    [ "${actual}" = "server-cert" ]
}

@test "server/StatefulSet: when server.serverCert.secretName!=null and global.tls.caCert.secretName=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/server-statefulset.yaml \
        --set 'global.tls.enabled=true' \
        --set 'server.serverCert.secretName=server-cert' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If server.serverCert.secretName is provided, global.tls.caCert must also be provided" ]]
}
#--------------------------------------------------------------------
# exposeGossipAndRPCPorts

@test "server/StatefulSet: server gossip and RPC ports are not exposed by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr )

  # Test that hostPort is not set for gossip ports
  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serflan-tcp")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serflan-udp")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serfwan-tcp")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serfwan-udp")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  # Test that hostPort is not set for rpc ports
  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "server")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  # Test that ADVERTISE_IP is being set to podIP
  local actual=$(echo "$object" |
    yq -r -c '.spec.template.spec.containers[0].env | map(select(.name == "ADVERTISE_IP"))' | tee /dev/stderr)
  [ "${actual}" = '[{"name":"ADVERTISE_IP","valueFrom":{"fieldRef":{"fieldPath":"status.podIP"}}}]' ]
}

@test "server/StatefulSet: server gossip and RPC ports can be exposed" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.exposeGossipAndRPCPorts=true' \
      . | tee /dev/stderr)

  # Test that hostPort is set for gossip ports
  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serflan-tcp")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "8301" ]

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serflan-udp")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "8301" ]

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serfwan-tcp")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "8302" ]

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serfwan-udp")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "8302" ]

  # Test that hostPort is set for rpc ports
  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "server")' | yq -r '.hostPort' | tee /dev/stderr)
  [ "${actual}" = "8300" ]

  # Test that ADVERTISE_IP is being set to hostIP
  local actual=$(echo "$object" |
    yq -r -c '.spec.template.spec.containers[0].env | map(select(.name == "ADVERTISE_IP"))' | tee /dev/stderr)
  [ "${actual}" = '[{"name":"ADVERTISE_IP","valueFrom":{"fieldRef":{"fieldPath":"status.hostIP"}}}]' ]

}

#--------------------------------------------------------------------
# serflan

@test "server/StatefulSet: server.ports.serflan.port is set to 8301 by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr )

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serflan-tcp")' | yq -r '.containerPort' | tee /dev/stderr)
  [ "${actual}" = "8301" ]

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serflan-udp")' | yq -r '.containerPort' | tee /dev/stderr)
  [ "${actual}" = "8301" ]

  local command=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)
}

@test "server/StatefulSet: server.ports.serflan.port can be customized" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.ports.serflan.port=9301' \
      . | tee /dev/stderr )

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serflan-tcp")' | yq -r '.containerPort' | tee /dev/stderr)
  [ "${actual}" = "9301" ]

  local actual=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].ports[] | select(.name == "serflan-udp")' | yq -r '.containerPort' | tee /dev/stderr)
  [ "${actual}" = "9301" ]

  local command=$(echo "$object" |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)
}

#--------------------------------------------------------------------
# extra-config

@test "server/StatefulSet: has extra-config volume" {
  cd `chart_dir`

  # check that the extra-config volume is defined
  local volume_name=$(helm template \
      -s templates/server-statefulset.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[] | select(.name == "extra-config") | .name' | tee /dev/stderr)
  [ "${volume_name}" = "extra-config" ]

  # check that the consul container mounts the volume at /consul/extra-config
  local mount_path=$(helm template \
      -s templates/server-statefulset.yaml \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[] | select(.name == "consul") | .volumeMounts[] | select(.name == "extra-config") | .mountPath' | tee /dev/stderr)
  [ "${mount_path}" = "/consul/extra-config" ]
}

#--------------------------------------------------------------------
# extraVolumes

@test "server/StatefulSet: adds extra volume" {
  cd `chart_dir`

  # Test that it defines it
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
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
      -s templates/server-statefulset.yaml  \
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
      -s templates/server-statefulset.yaml  \
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
      -s templates/server-statefulset.yaml  \
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
      -s templates/server-statefulset.yaml  \
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
      -s templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=configMap' \
      --set 'server.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | map(select(test("userconfig"))) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "server/StatefulSet: adds loadable volume" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=configMap' \
      --set 'server.extraVolumes[0].name=foo' \
      --set 'server.extraVolumes[0].load=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | map(select(test("/consul/userconfig/foo"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/StatefulSet: adds extra secret volume with items" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.extraVolumes[0].type=secret' \
      --set 'server.extraVolumes[0].name=foo' \
      --set 'server.extraVolumes[0].items[0].key=key' \
      --set 'server.extraVolumes[0].items[0].path=path' \
      . | tee /dev/stderr |
      yq -c '.spec.template.spec.volumes[] | select(.name == "userconfig-foo")' | tee /dev/stderr)
  [ "${actual}" = "{\"name\":\"userconfig-foo\",\"secret\":{\"secretName\":\"foo\",\"items\":[{\"key\":\"key\",\"path\":\"path\"}]}}" ]
}

#--------------------------------------------------------------------
# affinity

@test "server/StatefulSet: affinity not set with server.affinity=null" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.affinity=null' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .affinity? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: affinity set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.affinity | .podAntiAffinity? != null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "server/StatefulSet: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml \
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
      -s templates/server-statefulset.yaml \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/StatefulSet: specified priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --set 'server.priorityClassName=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "server/StatefulSet: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component") | del(."hasDNS")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "server/StatefulSet: extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.extraLabels.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "server/StatefulSet: multiple extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.extraLabels.foo=bar' \
      --set 'server.extraLabels.baz=qux' \
      . | tee /dev/stderr)
  local actualFoo=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  local actualBaz=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualFoo}" = "bar" ]
  [ "${actualBaz}" = "qux" ]
}

@test "server/StatefulSet: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "server/StatefulSet: multiple extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.extraLabels.foo=bar' \
      --set 'global.extraLabels.baz=qux' \
      . | tee /dev/stderr)
  local actualFoo=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  local actualBaz=$(echo "${actual}" | yq -r '.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualFoo}" = "bar" ]
  [ "${actualBaz}" = "qux" ]
  local actualTemplateFoo=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  local actualTemplateBaz=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualTemplateFoo}" = "bar" ]
  [ "${actualTemplateBaz}" = "qux" ]
}

#--------------------------------------------------------------------
# annotations

@test "server/StatefulSet: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."consul.hashicorp.com/config-checksum")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "server/StatefulSet: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# metrics

@test "server/StatefulSet: when global.metrics.enableAgentMetrics=true, adds prometheus scrape=true annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/scrape"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: when global.metrics.enableAgentMetrics=true, adds prometheus port=8500 annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/port"' | tee /dev/stderr)
  [ "${actual}" = "8500" ]
}

@test "server/StatefulSet: when global.metrics.enableAgentMetrics=true, adds prometheus path=/v1/agent/metrics annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/v1/agent/metrics" ]
}

@test "server/Statefulset: when global.metrics.enabled=true, and server annotation for prometheus path is specified, it uses the specified annotation rather than default." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'server.annotations=prometheus.io/path: /anew/path' \
      . | tee /dev/stderr |
      yq -s -r '.[0].spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/anew/path" ]
}


@test "server/StatefulSet: when global.metrics.enableAgentMetrics=true, adds prometheus scheme=http annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/scheme"' | tee /dev/stderr)
  [ "${actual}" = "http" ]
}

@test "server/StatefulSet: when global.metrics.enableAgentMetrics=true and global.tls.enabled=true, adds prometheus port=8501 annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.tls.enabled=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/port"' | tee /dev/stderr)
  [ "${actual}" = "8501" ]
}

@test "server/StatefulSet: when global.metrics.enableAgentMetrics=true and global.tls.enabled=true, adds prometheus scheme=https annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.tls.enabled=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/scheme"' | tee /dev/stderr)
  [ "${actual}" = "https" ]
}

@test "server/StatefulSet: when global.metrics.datadog.enabled=true, adds ad.datadoghq.com annotations" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/tolerate-unready"' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.logs"' | tee /dev/stderr)
  [ "${actual}" = '[{"source": "consul","consul_service": "consul-server"}]' ]

  local consul_checks=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.checks"' | tee /dev/stderr)

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.init_config | tee /dev/stderr)"
  [ "${actual}" = "{}" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].url | tee /dev/stderr)"
  [ "${actual}" = http://release-name-consul-server.default.svc:8500 ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].new_leader_checks | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].catalog_checks | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].auth_type | tee /dev/stderr)"
  [ "${actual}" = "basic" ]
}

@test "server/StatefulSet: when global.metrics.datadog.enabled=true and global.tls.enabled, adds tls altered ad.datadoghq.com annotations" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true'  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/tolerate-unready"' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.logs"' | tee /dev/stderr)
  [ "${actual}" = '[{"source": "consul","consul_service": "consul-server"}]' ]

  local consul_checks=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.checks"' | tee /dev/stderr)

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.init_config | tee /dev/stderr)"
  [ "${actual}" = "{}" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].url | tee /dev/stderr)"
  [ "${actual}" = "https://release-name-consul-server.default.svc:8501" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].tls_cert | tee /dev/stderr)"
  [ "${actual}" = "/etc/datadog-agent/conf.d/consul.d/certs/tls.crt" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].tls_private_key | tee /dev/stderr)"
  [ "${actual}" = "/etc/datadog-agent/conf.d/consul.d/certs/tls.key" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].tls_ca_cert | tee /dev/stderr)"
  [ "${actual}" = "/etc/datadog-agent/conf.d/consul.d/ca/tls.crt" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].new_leader_checks | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].catalog_checks | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].auth_type | tee /dev/stderr)"
  [ "${actual}" = "basic" ]
}

@test "server/StatefulSet: when global.metrics.datadog.enabled=true and global.acls.manageSystemACLs=true, adds ad.datadoghq.com annotations for datadog-agent-metrics-acl-token secret rendering" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local consul_checks=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.checks"' | tee /dev/stderr)

  local actual="$( echo "$consul_checks" | \
    jq -r .consul.instances | jq -r .[0].acl_token | tee /dev/stderr)"
  [ "${actual}" = "ENC[k8s_secret@default/default-datadog-agent-metrics-acl-token/token]" ]
}

@test "server/StatefulSet: when global.metrics.datadog.openMetricsPrometheus.enabled, applicable openmetrics annotation is set" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.openMetricsPrometheus.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local consul_checks=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.checks"' | tee /dev/stderr)

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.init_config | tee /dev/stderr)"
  [ "${actual}" = "{}" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].openmetrics_endpoint | tee /dev/stderr)"
  [ "${actual}" = "http://release-name-consul-server.default.svc:8500/v1/agent/metrics?format=prometheus" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].headers | tee /dev/stderr)"
  [ -n "${actual}" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].namespace | tee /dev/stderr)"
  [ "${actual}" = "default" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].metrics[0] | tee /dev/stderr)"
  [ "${actual}" = ".*" ]

}

@test "server/StatefulSet: when datadog.openMetricsPrometheus.enabled, applicable openmetrics annotation is set with tls url" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.tls.enabled=true'  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.openMetricsPrometheus.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local consul_checks=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.checks"' | tee /dev/stderr)

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.init_config | tee /dev/stderr)"
  [ "${actual}" = "{}" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].openmetrics_endpoint | tee /dev/stderr)"
  [ "${actual}" = "https://release-name-consul-server.default.svc:8501/v1/agent/metrics?format=prometheus" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].headers | tee /dev/stderr)"
  [ -n "${actual}" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].tls_cert | tee /dev/stderr)"
  [ "${actual}" = "/etc/datadog-agent/conf.d/consul.d/certs/tls.crt" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].tls_private_key | tee /dev/stderr)"
  [ "${actual}" = "/etc/datadog-agent/conf.d/consul.d/certs/tls.key" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].tls_ca_cert | tee /dev/stderr)"
  [ "${actual}" = "/etc/datadog-agent/conf.d/consul.d/ca/tls.crt" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].namespace | tee /dev/stderr)"
  [ "${actual}" = "default" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].metrics[0] | tee /dev/stderr)"
  [ "${actual}" = ".*" ]
}

@test "server/StatefulSet: when global.metrics.datadog.openMetricsPrometheus.enabled, applicable openmetrics annotation is set with acls.manageSystemACLs enabled" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.openMetricsPrometheus.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local consul_checks=$(echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.checks"' | tee /dev/stderr)

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.init_config | tee /dev/stderr)"
  [ "${actual}" = "{}" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].openmetrics_endpoint | tee /dev/stderr)"
  [ "${actual}" = "http://release-name-consul-server.default.svc:8500/v1/agent/metrics?format=prometheus" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r '.[0].headers["X-Consul-Token"]' | tee /dev/stderr)"
  [ "${actual}" = "ENC[k8s_secret@default/default-datadog-agent-metrics-acl-token/token]" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].namespace | tee /dev/stderr)"
  [ "${actual}" = "default" ]

  local actual="$( echo "$consul_checks" | \
    jq -r .openmetrics.instances | jq -r .[0].metrics[0] | tee /dev/stderr)"
  [ "${actual}" = ".*" ]

}

@test "server/StatefulSet: consul metrics exclusion annotation when using metrics.datadog.dogstatsd.enabled=true" {
  cd `chart_dir`
  local annotations=$(helm template \
      -s templates/server-statefulset.yaml \
      --set 'global.image=hashicorp/consul-enterprise:1.17.0-ent' \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.dogstatsd.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations' | tee /dev/stderr)

  local actual=$( echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.checks"' | tee /dev/stderr )
  [ -n "${actual}" ]

  local actual=$( echo "$annotations" | \
    yq -r '."ad.datadoghq.com/consul.metrics_exclude"' | tee /dev/stderr )
  [ "${actual}" = "true" ]
}


@test "server/StatefulSet: datadog unified tagging labels get added when global.metrics.datadog.enabled=true" {
  cd `chart_dir`
  local labels=$(helm template \
      -s templates/server-statefulset.yaml \
      --set 'global.image=hashicorp/consul-enterprise:1.17.0-ent' \
      --set 'global.metrics.enabled=true'  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels' | tee /dev/stderr)

  local actual=$( echo "$labels" | \
    yq -r '."tags.datadoghq.com/version"' | tee /dev/stderr )
  [ "${actual}" = "1.17.0-ent" ]

  local actual=$( echo "$labels" | \
    yq -r '."tags.datadoghq.com/env"' | tee /dev/stderr )
  [ "${actual}" = "consul" ]

  local actual=$( echo "$labels" | \
    yq -r '."tags.datadoghq.com/service"' | tee /dev/stderr )
  [ "${actual}" = "consul-server" ]
}

@test "server/StatefulSet: datadog unix socket path name rendering for hostPath volume and volumeMount using default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --set 'global.metrics.enabled=true'  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.dogstatsd.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[] | select(.name=="dsdsocket") | .hostPath.path' | tee /dev/stderr)

  [ "${actual}" = "/var/run/datadog" ]
}

@test "server/StatefulSet: datadog unix socket path name rendering for hostPath volume and volumeMount using non default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --set 'global.metrics.enabled=true'  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true' \
      --set 'global.metrics.datadog.dogstatsd.enabled=true' \
      --set 'global.metrics.datadog.dogstatsd.dogstatsdAddr="/this/otherpath/datadog/dsd.socket"' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[] | select(.name=="dsdsocket") | .hostPath.path' | tee /dev/stderr)

  [ "${actual}" = "/this/otherpath/datadog" ]
}

#--------------------------------------------------------------------
# config-configmap

@test "server/StatefulSet: adds config-checksum annotation when extraConfig is blank" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  [ "${actual}" = a4771bea366d4a6ee9037572665dc4040519dc22e9b0ff3463a263aab13675b8 ]
}

@test "server/StatefulSet: adds config-checksum annotation when extraConfig is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.extraConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  [ "${actual}" = c6b872933263bf5fe847d61e638035637d2db89edf31ad25d0aaeaa5261649c9 ]
}

@test "server/StatefulSet: adds config-checksum annotation when config is updated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  [ "${actual}" = 576044232d6181bca69628af87c12f15311ebd3f0ab700e112b3e1dea9225125 ]
}

#--------------------------------------------------------------------
# server extraConfig validation

@test "server/Statefulset: Validate enable_debug extraConfig for Consul Helm chart" {
    cd `chart_dir`
    run helm template \
      -s templates/server-statefulset.yaml \
      --set global.metrics.enabled=true \
      --set global.metrics.enableAgentMetrics=true \
      --set server.extraConfig=enable_debug=true \
      .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "The enable_debug key is present in extra-from-values.json. Use server.enableAgentDebug to set this value." ]]
}

@test "server/Statefulset: Validate disable_hostname extraConfig for Consul Helm chart" {
    cd `chart_dir`
    run helm template \
      -s templates/server-statefulset.yaml \
      --set global.metrics.enabled=true \
      --set global.metrics.enableAgentMetrics=true \
      --set server.extraConfig=telemetry.disable_hostname=true \
      .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "The disable_hostname key is present in extra-from-values.json. Use global.metrics.disableAgentHostName to set this value." ]]
}

@test "server/Statefulset: Validate enable_host_metrics extraConfig for Consul Helm chart" {
    cd `chart_dir`
    run helm template \
      -s templates/server-statefulset.yaml \
      --set global.metrics.enabled=true \
      --set global.metrics.enableAgentMetrics=true \
      --set server.extraConfig=telemetry.enable_host_metrics=true \
      .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "The enable_host_metrics key is present in extra-from-values.json. Use global.metrics.enableHostMetrics to set this value." ]]
}

@test "server/Statefulset: Validate prefix_filter extraConfig for Consul Helm chart" {
    cd `chart_dir`
    run helm template \
      -s templates/server-statefulset.yaml \
      --set global.metrics.enabled=true \
      --set global.metrics.enableAgentMetrics=true \
      --set server.extraConfig=telemetry.prefix_filter=["+consul.rpc.server.call"] \
      .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "The prefix_filter key is present in extra-from-values.json. Use global.metrics.prefix_filter to set this value." ]]
}

@test "server/Statefulset: Validate dogstatsd_tags extraConfig for Consul Helm chart" {
    cd `chart_dir`
    run helm template \
      -s templates/server-statefulset.yaml \
      --set global.metrics.enabled=true \
      --set global.metrics.enableAgentMetrics=true \
      --set global.metrics.datadog.dogstatsd.enabled=true \
      --set server.extraConfig=telemetry.dogstatsd_tags='[\"source:consul-server\"\,\"consul_service:consul\"]' \
      .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "The dogstatsd_tags key is present in extra-from-values.json. Use global.metrics.datadog.dogstatsd.dogstatsdTags to set this value." ]]
}

@test "server/Statefulset: Validate dogstatsd_addr extraConfig for Consul Helm chart" {
    cd `chart_dir`
    run helm template \
      -s templates/server-statefulset.yaml \
      --set global.metrics.enabled=true \
      --set global.metrics.enableAgentMetrics=true \
      --set global.metrics.datadog.dogstatsd.enabled=true \
      --set server.extraConfig=telemetry.dogstatsd_addr="localhost:8125" \
      .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "The dogstatsd_addr key is present in extra-from-values.json. Use global.metrics.datadog.dogstatsd.dogstatsd_addr to set this value." ]]
}

#--------------------------------------------------------------------
# tolerations

@test "server/StatefulSet: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .tolerations? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.tolerations=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# topologySpreadConstraints

@test "server/StatefulSet: topologySpreadConstraints not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .topologySpreadConstraints? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: topologySpreadConstraints can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.topologySpreadConstraints=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.topologySpreadConstraints == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.openshift.enabled & server.securityContext

@test "server/StatefulSet: setting server.disableFsGroupSecurityContext fails" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.disableFsGroupSecurityContext=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "server.disableFsGroupSecurityContext has been removed. Please use global.openshift.enabled instead." ]]
}

@test "server/StatefulSet: securityContext is not set when global.openshift.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

#--------------------------------------------------------------------
# server.securityContext

@test "server/StatefulSet: sets default security context settings" {
  cd `chart_dir`
  local security_context=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)

  local actual=$(echo "$security_context" | yq -r .runAsNonRoot)
  [ "${actual}" = "true" ]

  local actual=$(echo "$security_context" | yq -r .fsGroup)
  [ "${actual}" = "1000" ]

  local actual=$(echo "$security_context" | yq -r .runAsUser)
  [ "${actual}" = "100" ]

  local actual=$(echo "$security_context" | yq -r .runAsGroup)
  [ "${actual}" = "1000" ]
}

@test "server/StatefulSet: can overwrite security context settings" {
  cd `chart_dir`
  local security_context=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.securityContext.runAsNonRoot=false' \
      --set 'server.securityContext.privileged=true' \
      --set 'server.securityContext.runAsGroup=0' \
      --set 'server.securityContext.runAsUser=0' \
      --set 'server.securityContext.fsGroup=0' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)

  local actual=$(echo "$security_context" | yq -r .runAsNonRoot)
  [ "${actual}" = "false" ]

  local actual=$(echo "$security_context" | yq -r .privileged)
  [ "${actual}" = "true" ]

    local actual=$(echo "$security_context" | yq -r .fsGroup)
  [ "${actual}" = "0" ]

  local actual=$(echo "$security_context" | yq -r .runAsUser)
  [ "${actual}" = "0" ]

  local actual=$(echo "$security_context" | yq -r .runAsGroup)
  [ "${actual}" = "0" ]
}

#--------------------------------------------------------------------
# server.containerSecurityContext

@test "server/StatefulSet: Can set container level securityContexts" {
  cd `chart_dir`
  local manifest=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.containerSecurityContext.server.privileged=false' \
      . | tee /dev/stderr)

  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.containers | map(select(.name == "consul")) | .[0].securityContext.privileged')
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# global.openshift.enabled && server.containerSecurityContext

@test "server/StatefulSet: Can set container level securityContexts when global.openshift.enabled=true" {
  cd `chart_dir`
  local manifest=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.openshift.enabled=true' \
      --set 'server.containerSecurityContext.server.privileged=false' \
      . | tee /dev/stderr)

  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.containers | map(select(.name == "consul")) | .[0].securityContext.privileged')
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# global.openshift.enabled

@test "server/StatefulSet: restricted container securityContexts are set when global.openshift.enabled=true" {
  cd `chart_dir`
  local manifest=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr)

  local expected=$(echo '{
    "allowPrivilegeEscalation": false,
    "capabilities": {
      "drop": ["ALL"],
      "add": ["NET_BIND_SERVICE"]
    },
    "readOnlyRootFilesystem": true,
    "runAsNonRoot": true,
    "seccompProfile": {
      "type": "RuntimeDefault"
    }
  }')

  # Check consul container
  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.containers | map(select(.name == "consul")) | .[0].securityContext')
  local equal=$(jq -n --argjson a "$actual" --argjson b "$expected" '$a == $b')
  [ "$equal" == "true" ]

  # Check locality-init container
  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.initContainers | map(select(.name == "locality-init")) | .[0].securityContext')
  local equal=$(jq -n --argjson a "$actual" --argjson b "$expected" '$a == $b')
  [ "$equal" == "true" ]
}

#--------------------------------------------------------------------
# global.openshift.enabled = false

@test "server/StatefulSet: restricted container securityContexts are set by default" {
  cd `chart_dir`
  local manifest=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr)

  local expected=$(echo '{
    "allowPrivilegeEscalation": false,
    "capabilities": {
      "drop": ["ALL"],
      "add": ["NET_BIND_SERVICE"]
    },
    "readOnlyRootFilesystem": true,
    "runAsNonRoot": true,
    "seccompProfile": {
      "type": "RuntimeDefault"
    },
    "runAsUser": 100
  }')

  # Check consul container
  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.containers | map(select(.name == "consul")) | .[0].securityContext')
  local equal=$(jq -n --argjson a "$actual" --argjson b "$expected" '$a == $b')
  [ "$equal" == "true" ]

  # Check locality-init container
  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.initContainers | map(select(.name == "locality-init")) | .[0].securityContext')
  local equal=$(jq -n --argjson a "$actual" --argjson b "$expected" '$a == $b')
  [ "$equal" == "true" ]
}

#--------------------------------------------------------------------
# gossip encryption

@test "server/StatefulSet: gossip encryption disabled in server StatefulSet by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "server/StatefulSet: gossip encryption autogeneration properly sets secretName and secretKey" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.gossipEncryption.autoGenerate=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | .valueFrom.secretKeyRef | [.name=="release-name-consul-gossip-encryption-key", .key="key"] | all' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: gossip encryption key is passed via the -encrypt flag" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/server-statefulset.yaml \
    --set 'global.gossipEncryption.autoGenerate=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | any(contains("-encrypt=\"${GOSSIP_KEY}\""))' \
    | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: fail if global.gossipEncyption.gossipEncryption.secretName is set but not global.gossipEncyption.secretKey" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.secretName=bar' .
  [[ "$output" =~ "gossipEncryption.secretKey and secretName must both be specified." ]]
}

@test "server/StatefulSet: fail if global.gossipEncyption.gossipEncryption.secretKey is set but not global.gossipEncyption.secretName" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.secretKey=bar' .
  [[ "$output" =~ "gossipEncryption.secretKey and secretName must both be specified." ]]
}

@test "server/StatefulSet: gossip environment variable present in server StatefulSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: encrypt CLI option not present in server StatefulSet when encryption disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | join(" ") | contains("encrypt")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: encrypt CLI option present in server StatefulSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
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
      -s templates/server-statefulset.yaml  \
      --set 'server.extraEnvironmentVars.custom_proxy=fakeproxy' \
      --set 'server.extraEnvironmentVars.no_proxy=custom_no_proxy' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r 'map(select(.name == "custom_proxy")) | .[0].value' | tee /dev/stderr)
  [ "${actual}" = "fakeproxy" ]

  local actual=$(echo $object |
      yq -r 'map(select(.name == "no_proxy")) | .[0].value' | tee /dev/stderr)
  [ "${actual}" = "custom_no_proxy" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "server/StatefulSet: CA volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: server cert volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-server-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: CA volume mounted when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: server certificate volume mounted when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-server-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: port 8501 is not exposed when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8501)' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "server/StatefulSet: port 8501 is exposed when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8501)' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: port 8500 is still exposed when httpsOnly is not enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8500)' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: port 8500 is not exposed when httpsOnly is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8500)' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "server/StatefulSet: readiness checks are over HTTP when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("http://127.0.0.1:8500")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: readiness checks are over HTTPS when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("https://127.0.0.1:8501")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: sets Consul environment variables when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "https://localhost:8501" ]

  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
    [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "server/StatefulSet: sets Consul environment variables when global.tls.enabled and global.secretsBackend.vault.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
    [ "${actual}" = "/vault/secrets/serverca.crt" ]
}

@test "server/StatefulSet: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/server-statefulset.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo-ca-cert' \
      --set 'global.tls.caCert.secretKey=key' \
      --set 'global.tls.caKey.secretName=foo-ca-key' \
      --set 'global.tls.caKey.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name=="consul-ca-cert")' | tee /dev/stderr)

  # check that the provided ca cert secret is attached as a volume
  local actual
  actual=$(echo $ca_cert_volume | jq -r '.secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-cert" ]

  # check that the volume uses the provided secret key
  actual=$(echo $ca_cert_volume | jq -r '.secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]
}

#--------------------------------------------------------------------
# global.federation.enabled

@test "server/StatefulSet: fails when federation.enabled=true and tls.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.federation.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.federation.enabled is true, global.tls.enabled must be true because federation is only supported with TLS enabled" ]]
}

#--------------------------------------------------------------------
# global.acls.bootstrapToken

@test "server/StatefulSet: when global.acls.bootstrapToken.secretKey!=null and global.acls.bootstrapToken.secretName=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/server-statefulset.yaml \
        --set 'global.acls.bootstrapToken.secretName=' \
        --set 'global.acls.bootstrapToken.secretKey=enterpriselicense' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "both global.acls.bootstrapToken.secretKey and global.acls.bootstrapToken.secretName must be set if one of them is provided." ]]
}

@test "server/StatefulSet: when global.acls.bootstrapToken.secretName!=null and global.acls.bootstrapToken.secretKey=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/server-statefulset.yaml \
        --set 'global.acls.bootstrapToken.secretName=foo' \
        --set 'global.acls.bootstrapToken.secretKey=' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "both global.acls.bootstrapToken.secretKey and global.acls.bootstrapToken.secretName must be set if one of them is provided." ]]
}

@test "server/StatefulSet: acl bootstrap token config is not set by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("ACL_BOOTSTRAP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the ACL_BOOTSTRAP_TOKEN environment variable is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].env | map(select(.name == "ACL_BOOTSTRAP_TOKEN")) | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: acl bootstrap token config is set when acls.bootstrapToken.secretKey and secretName are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.acls.enabled=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \
      --set 'global.acls.bootstrapToken.secretKey=key' \
      . | tee /dev/stderr)

  # Test the flag is set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("-hcl=\"acl { tokens { initial_management = \\\"${ACL_BOOTSTRAP_TOKEN}\\\" } }\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the ACL_BOOTSTRAP_TOKEN environment variable is set.
  local actual=$(echo "$object" |
    yq -r -c '.spec.template.spec.containers[0].env | map(select(.name == "ACL_BOOTSTRAP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = '[{"name":"ACL_BOOTSTRAP_TOKEN","valueFrom":{"secretKeyRef":{"name":"name","key":"key"}}}]' ]
}

#--------------------------------------------------------------------
# global.acls.replicationToken

@test "server/StatefulSet: acl replication token config is not set by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("ACL_REPLICATION_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the ACL_REPLICATION_TOKEN environment variable is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].env | map(select(.name == "ACL_REPLICATION_TOKEN")) | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: acl replication token config is not set when acls.replicationToken.secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.acls.replicationToken.secretName=name' \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("ACL_REPLICATION_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the ACL_REPLICATION_TOKEN environment variable is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].env | map(select(.name == "ACL_REPLICATION_TOKEN")) | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: acl replication token config is not set when acls.replicationToken.secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.acls.replicationToken.secretKey=key' \
      . | tee /dev/stderr)

  # Test the flag is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("ACL_REPLICATION_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the ACL_REPLICATION_TOKEN environment variable is not set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].env | map(select(.name == "ACL_REPLICATION_TOKEN")) | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: acl replication token config is set when acls.replicationToken.secretKey and secretName are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.acls.replicationToken.secretName=name' \
      --set 'global.acls.replicationToken.secretKey=key' \
      . | tee /dev/stderr)

  # Test the flag is set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("-hcl=\"acl { tokens { agent = \\\"${ACL_REPLICATION_TOKEN}\\\", replication = \\\"${ACL_REPLICATION_TOKEN}\\\" } }\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the ACL_REPLICATION_TOKEN environment variable is set.
  local actual=$(echo "$object" |
    yq -r -c '.spec.template.spec.containers[0].env | map(select(.name == "ACL_REPLICATION_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = '[{"name":"ACL_REPLICATION_TOKEN","valueFrom":{"secretKeyRef":{"name":"name","key":"key"}}}]' ]
}

#--------------------------------------------------------------------
# license-autoload

@test "server/StatefulSet: adds volume for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","secret":{"secretName":"foo"}}' ]
}

@test "server/StatefulSet: adds volume mount for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","mountPath":"/consul/license","readOnly":true}' ]
}

@test "server/StatefulSet: adds env var for license path when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"CONSUL_LICENSE_PATH","value":"/consul/license/bar"}' ]
}

@test "server/StatefulSet: when global.enterpriseLicense.secretKey!=null and global.enterpriseLicense.secretName=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/server-statefulset.yaml \
        --set 'global.enterpriseLicense.secretName=' \
        --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "enterpriseLicense.secretKey and secretName must both be specified." ]]
}

@test "server/StatefulSet: when global.enterpriseLicense.secretName!=null and global.enterpriseLicense.secretKey=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/server-statefulset.yaml \
        --set 'global.enterpriseLicense.secretName=foo' \
        --set 'global.enterpriseLicense.secretKey=' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "enterpriseLicense.secretKey and secretName must both be specified." ]]
}
#--------------------------------------------------------------------
# extraContainers

@test "server/StatefulSet: adds extra container" {
  cd `chart_dir`

  # Test that it defines the extra container
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.extraContainers[0].image=test-image' \
      --set 'server.extraContainers[0].name=test-container' \
      --set 'server.extraContainers[0].ports[0].name=test-port' \
      --set 'server.extraContainers[0].ports[0].containerPort=9410' \
      --set 'server.extraContainers[0].ports[0].protocol=TCP' \
      --set 'server.extraContainers[0].env[0].name=TEST_ENV' \
      --set 'server.extraContainers[0].env[0].value=test_env_value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[] | select(.name == "test-container")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "test-container" ]

  local actual=$(echo $object |
      yq -r '.image' | tee /dev/stderr)
  [ "${actual}" = "test-image" ]

  local actual=$(echo $object |
      yq -r '.ports[0].name' | tee /dev/stderr)
  [ "${actual}" = "test-port" ]

  local actual=$(echo $object |
      yq -r '.ports[0].containerPort' | tee /dev/stderr)
  [ "${actual}" = "9410" ]

  local actual=$(echo $object |
      yq -r '.ports[0].protocol' | tee /dev/stderr)
  [ "${actual}" = "TCP" ]

  local actual=$(echo $object |
      yq -r '.env[0].name' | tee /dev/stderr)
  [ "${actual}" = "TEST_ENV" ]

  local actual=$(echo $object |
      yq -r '.env[0].value' | tee /dev/stderr)
  [ "${actual}" = "test_env_value" ]

}

@test "server/StatefulSet: adds two extra containers" {
  cd `chart_dir`

  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.extraContainers[0].image=test-image' \
      --set 'server.extraContainers[0].name=test-container' \
      --set 'server.extraContainers[1].image=test-image' \
      --set 'server.extraContainers[1].name=test-container-2' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers | length' | tee /dev/stderr)

  [ "${object}" = 3 ]

}

@test "server/StatefulSet: no extra containers added by default" {
  cd `chart_dir`

  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers | length' | tee /dev/stderr)

  [ "${object}" = 1 ]
}

#--------------------------------------------------------------------
# vault integration

@test "server/StatefulSet: fail when vault is enabled but the consulServerRole is not provided" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulClientRole=test' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.secretsBackend.vault.consulServerRole must be provided if global.secretsBackend.vault.enabled=true" ]]
}

@test "server/StatefulSet: fail when vault, tls are enabled but no caCert provided" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.tls.enabled=true' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.tls.caCert.secretName must be provided if global.tls.enabled=true and global.secretsBackend.vault.enabled=true." ]]
}

@test "server/StatefulSet: vault annotations not set by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/role"] | length > 0 ' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: vault annotations added when vault is enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "test" ]
}

@test "server/StatefulSet: vault gossip annotations are correct when enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.gossipEncryption.secretName=path/to/secret' \
    --set 'global.gossipEncryption.secretKey=gossip' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-gossip.txt"]' | tee /dev/stderr)
  [ "${actual}" = "path/to/secret" ]
  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-gossip.txt"]' | tee /dev/stderr)
  local actual="$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-gossip.txt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"path/to/secret\" -}}\n{{- .Data.data.gossip -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "server/StatefulSet: vault no GOSSIP_KEY env variable and command defines GOSSIP_KEY" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.gossipEncryption.secretName=a/b/c/d' \
    --set 'global.gossipEncryption.secretKey=gossip' \
    . | tee /dev/stderr |
      yq -r '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo $object |
    yq -r '.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object |
    yq -r '.containers[] | select(.name=="consul") | .command | any(contains("GOSSIP_KEY="))' \
      | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check that the volume is defined.
  local actual=$(echo $object |
    yq -r '.spec.volumes[] | select(.name=="vault-ca")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  # Check that the volume mount is added.
  local actual=$(echo $object |
    yq -r '.spec.containers[] | select(.name=="consul").volumeMounts[] | select(.name=="vault-ca")' \
      | tee /dev/stderr)
  [ "${actual}" = "" ]

  # Check that Vault agent annotations are added.
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "vns" ]
}

@test "server/StatefulSet: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set without vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/agent-extra-secret: bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "vns" ]
}

@test "server/StatefulSet: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/namespace: bar' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "bar" ]
}

@test "server/StatefulSet: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check that the volume is defined.
  local actual=$(echo $object |
    yq -r '.spec.volumes[] | select(.name=="vault-ca")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  # Check that the volume mount is added.
  local actual=$(echo $object |
    yq -r '.spec.containers[] | select(.name=="consul").volumeMounts[] | select(.name=="vault-ca")' \
      | tee /dev/stderr)
  [ "${actual}" = "" ]

  # Check that Vault agent annotations are added.
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check that the volume is defined.
  local actual=$(echo $object |
    yq -r '.spec.volumes[] | select(.name=="vault-ca")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  # Check that the volume mount is added.
  local actual=$(echo $object |
    yq -r '.spec.containers[] | select(.name=="consul").volumeMounts[] | select(.name=="vault-ca")' \
      | tee /dev/stderr)
  [ "${actual}" = "" ]

  # Check that Vault agent annotations are added.
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check that the volume is defined.
  local actual=$(echo $object |
    yq -r '.spec.volumes[] | select(.name=="vault-ca").secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "ca" ]

  # Check that the volume mount is added.
  local actual=$(echo $object |
    yq -r '.spec.containers[] | select(.name=="consul").volumeMounts[] | select(.name=="vault-ca")' \
      | tee /dev/stderr)
  [ "${actual}" != "" ]

  # Check that Vault agent annotations are added.
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

@test "server/StatefulSet: vault tls annotations are set when tls is enabled and command modified correctly" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.datacenter=dc2' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
    --set 'server.serverCert.secretName=pki_int/issue/test' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki_int/cert/ca" ]

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/cert/ca\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-servercert.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki_int/issue/test" ]

  local actual=$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-servercert.crt"]' | tee /dev/stderr)
  local expected=$'{{- with secret \"pki_int/issue/test\" \"common_name=server.dc2.consul\"\n\"alt_names=localhost,release-name-consul-server,*.release-name-consul-server,*.release-name-consul-server.default,release-name-consul-server.default,*.release-name-consul-server.default.svc,release-name-consul-server.default.svc,*.server.dc2.consul\" \"ip_sans=127.0.0.1\" -}}\n{{- .Data.certificate -}}\n{{- if .Data.ca_chain -}}\n{{- $lastintermediatecertindex := len .Data.ca_chain | subtract 1 -}}\n{{ range $index, $cacert := .Data.ca_chain }}\n{{ if (lt $index $lastintermediatecertindex) }}\n{{ $cacert }}\n{{ end }}\n{{ end }}\n{{- end -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-servercert.key"]' | tee /dev/stderr)"
  [ "${actual}" = "pki_int/issue/test" ]

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-servercert.key"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/issue/test\" \"common_name=server.dc2.consul\"\n\"alt_names=localhost,release-name-consul-server,*.release-name-consul-server,*.release-name-consul-server.default,release-name-consul-server.default,*.release-name-consul-server.default.svc,release-name-consul-server.default.svc,*.server.dc2.consul\" \"ip_sans=127.0.0.1\" -}}\n{{- .Data.private_key -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "server/StatefulSet: tls related volumes not attached when tls is enabled on vault" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'server.serverCert.secretName=pki_int/issue/test' \
    --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
    . | tee /dev/stderr |
      yq -r '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo $object |
    yq -r '.volumes[] | select(.name == "consul-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object |
    yq -r '.containers[0].volumeMounts[] | select(.name == "consul-ca-key")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "server/StatefulSet: vault - can set additional alt_names on server cert when tls is enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.datacenter=dc2' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
    --set 'server.serverCert.secretName=pki_int/issue/test' \
    --set 'global.tls.serverAdditionalDNSSANs[0]=*.foo.com' \
    --set 'global.tls.serverAdditionalDNSSANs[1]=*.bar.com' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-servercert.crt"]' | tee /dev/stderr)
  local expected=$'{{- with secret \"pki_int/issue/test\" \"common_name=server.dc2.consul\"\n\"alt_names=localhost,release-name-consul-server,*.release-name-consul-server,*.release-name-consul-server.default,release-name-consul-server.default,*.release-name-consul-server.default.svc,release-name-consul-server.default.svc,*.server.dc2.consul,*.foo.com,*.bar.com\" \"ip_sans=127.0.0.1\" -}}\n{{- .Data.certificate -}}\n{{- if .Data.ca_chain -}}\n{{- $lastintermediatecertindex := len .Data.ca_chain | subtract 1 -}}\n{{ range $index, $cacert := .Data.ca_chain }}\n{{ if (lt $index $lastintermediatecertindex) }}\n{{ $cacert }}\n{{ end }}\n{{ end }}\n{{- end -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-servercert.key"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/issue/test\" \"common_name=server.dc2.consul\"\n\"alt_names=localhost,release-name-consul-server,*.release-name-consul-server,*.release-name-consul-server.default,release-name-consul-server.default,*.release-name-consul-server.default.svc,release-name-consul-server.default.svc,*.server.dc2.consul,*.foo.com,*.bar.com\" \"ip_sans=127.0.0.1\" -}}\n{{- .Data.private_key -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "server/StatefulSet: vault - can set additional ip_sans on server cert when tls is enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.datacenter=dc2' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
    --set 'server.serverCert.secretName=pki_int/issue/test' \
    --set 'global.tls.serverAdditionalIPSANs[0]=1.1.1.1' \
    --set 'global.tls.serverAdditionalIPSANs[1]=2.2.2.2' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-servercert.crt"]' | tee /dev/stderr)
  local expected=$'{{- with secret \"pki_int/issue/test\" \"common_name=server.dc2.consul\"\n\"alt_names=localhost,release-name-consul-server,*.release-name-consul-server,*.release-name-consul-server.default,release-name-consul-server.default,*.release-name-consul-server.default.svc,release-name-consul-server.default.svc,*.server.dc2.consul\" \"ip_sans=127.0.0.1,1.1.1.1,2.2.2.2\" -}}\n{{- .Data.certificate -}}\n{{- if .Data.ca_chain -}}\n{{- $lastintermediatecertindex := len .Data.ca_chain | subtract 1 -}}\n{{ range $index, $cacert := .Data.ca_chain }}\n{{ if (lt $index $lastintermediatecertindex) }}\n{{ $cacert }}\n{{ end }}\n{{ end }}\n{{- end -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-servercert.key"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/issue/test\" \"common_name=server.dc2.consul\"\n\"alt_names=localhost,release-name-consul-server,*.release-name-consul-server,*.release-name-consul-server.default,release-name-consul-server.default,*.release-name-consul-server.default.svc,release-name-consul-server.default.svc,*.server.dc2.consul\" \"ip_sans=127.0.0.1,1.1.1.1,2.2.2.2\" -}}\n{{- .Data.private_key -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "server/StatefulSet: vault enterprise license annotations are correct when enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.enterpriseLicense.secretName=path/to/enterpriselicensesecret' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-enterpriselicense.txt"]' | tee /dev/stderr)
  [ "${actual}" = "path/to/enterpriselicensesecret" ]
  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-enterpriselicense.txt"]' | tee /dev/stderr)
  local actual="$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-enterpriselicense.txt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"path/to/enterpriselicensesecret\" -}}\n{{- .Data.data.enterpriselicense -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "server/StatefulSet: vault CONSUL_LICENSE_PATH is set to /vault/secrets/enterpriselicense.txt" {
  cd `chart_dir`
  local env=$(helm template \
     -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.enterpriseLicense.secretName=a/b/c/d' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
    . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_LICENSE_PATH") | .value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/enterpriselicense.txt" ]
}

@test "server/StatefulSet: vault does not add volume for license secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.enterpriseLicense.secretName=a/b/c/d' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "server/StatefulSet: vault does not add volume mount for license secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.enterpriseLicense.secretName=a/b/c/d' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# Vault agent annotations

@test "server/StatefulSet: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."consul.hashicorp.com/config-checksum") |
      del(."vault.hashicorp.com/agent-inject") |
      del(."vault.hashicorp.com/role")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "server/StatefulSet: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# Vault bootstrap token

@test "server/StatefulSet: vault bootstrap token is configured when secret provided" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.manageSystemACLsRole=aclrole' \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.acls.bootstrapToken.secretName=vault/bootstrap-token' \
    --set 'global.acls.bootstrapToken.secretKey=token' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check that Vault annotations are set.
  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-bootstrap-token-config.hcl"]' | tee /dev/stderr)"
  [ "${actual}" = "vault/bootstrap-token" ]

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-bootstrap-token-config.hcl"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"vault/bootstrap-token\" -}}\nacl { tokens { initial_management = \"{{- .Data.data.token -}}\" }}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  # Check that path to Vault secret config is provided to the command.
  local actual="$(echo $object | yq -r '.spec.containers[] | select(.name=="consul").command | any(contains("-config-file=/vault/secrets/bootstrap-token-config.hcl"))' | tee /dev/stderr)"
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault replication token

@test "server/StatefulSet: vault replication token is configured when secret provided and createReplicationToken is false" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.acls.replicationToken.secretName=vault/replication-token' \
    --set 'global.acls.replicationToken.secretKey=token' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  # Check that Vault annotations are set.
  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-secret-replication-token-config.hcl"]' | tee /dev/stderr)"
  [ "${actual}" = "vault/replication-token" ]

  local actual="$(echo $object |
      yq -r '.metadata.annotations["vault.hashicorp.com/agent-inject-template-replication-token-config.hcl"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"vault/replication-token\" -}}\nacl { tokens { agent = \"{{- .Data.data.token -}}\", replication = \"{{- .Data.data.token -}}\" }}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  # Check that ACL_REPLICATION_TOKEN env var is not provided.
  local actual="$(echo $object | yq -r '.spec.containers[] | select(.name=="consul").env[] | select(.name=="ACL_REPLICATION_TOKEN")' | tee /dev/stderr)"
  [ "${actual}" = "" ]

  # Check that path to Vault secret config is provided to the command.
  local actual="$(echo $object | yq -r '.spec.containers[] | select(.name=="consul").command | any(contains("-config-file=/vault/secrets/replication-token-config.hcl"))' | tee /dev/stderr)"
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.cloud

@test "server/StatefulSet: cloud config is not set in command when global.cloud.enabled is not set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr)

  # Test the flag is set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[] | select(.name == "consul") | .command | any(contains("-hcl=\"cloud { resource_id = \\\"${HCP_RESOURCE_ID}\\\" }\""))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  # Test the HCP_RESOURCE_ID environment variable is set.
  local envvar=$(echo "$object" |
    yq -r -c '.spec.template.spec.containers[] | select(.name == "consul") | .env | select(.name == "HCP_RESOURCE_ID")' | tee /dev/stderr)
  [ "${envvar}" = "" ]
}

@test "server/StatefulSet: does not create HCP_RESOURCE_ID, HCP_CLIENT_ID, HCP_CLIENT_SECRET, HCP_AUTH_URL, HCP_SCADA_ADDRESS, and HCP_API_HOSTNAME envvars in consul container when global.cloud.enabled is not set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml \
    . | tee /dev/stderr )

  local container=$(echo "$object" |
    yq -r '.spec.template.spec.containers[] | select(.name == "consul")' | tee /dev/stderr)
  

  local envvar=$(echo "$container" |
    yq -r '.env[] | select(.name == "HCP_CLIENT_ID")' | tee /dev/stderr)
  [ "${envvar}" = "" ]

  envvar=$(echo "$container" |
    yq -r '.env[] | select(.name == "HCP_CLIENT_SECRET")' | tee /dev/stderr)
  [ "${envvar}" = "" ]

  envvar=$(echo "$container" |
    yq -r '.env[] | select(.name == "HCP_RESOURCE_ID")' | tee /dev/stderr)
  [ "${envvar}" = "" ]

  envvar=$(echo "$container" |
    yq -r '.env[] | select(.name == "HCP_AUTH_URL")' | tee /dev/stderr)
  [ "${envvar}" = "" ]

  envvar=$(echo "$container" |
    yq -r '.env[] | select(.name == "HCP_API_HOSTNAME")' | tee /dev/stderr)
  [ "${envvar}" = "" ]

  envvar=$(echo "$container" |
    yq -r '.env[] | select(.name == "HCP_SCADA_ADDRESS")' | tee /dev/stderr)
  [ "${envvar}" = "" ]

}

@test "server/StatefulSet: cloud config is set in command when global.cloud.enabled and global.cloud.resourceId are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[] | select(.name == "consul") | .command | any(contains("-hcl=\"cloud { resource_id = \\\"${HCP_RESOURCE_ID}\\\" }\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}


@test "server/StatefulSet: creates HCP_RESOURCE_ID, HCP_CLIENT_ID, HCP_CLIENT_SECRET envvars in consul container when global.cloud.enabled is true" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml \
    --set 'global.cloud.enabled=true' \
    --set 'global.cloud.clientId.secretName=client-id-name' \
    --set 'global.cloud.clientId.secretKey=client-id-key' \
    --set 'global.cloud.clientSecret.secretName=client-secret-name' \
    --set 'global.cloud.clientSecret.secretKey=client-secret-key' \
    --set 'global.cloud.resourceId.secretName=resource-id-name' \
    --set 'global.cloud.resourceId.secretKey=resource-id-key' \
    . | tee /dev/stderr )

  local container=$(echo "$object" |
    yq -r '.spec.template.spec.containers[] | select(.name == "consul")' | tee /dev/stderr)
  
  # HCP_CLIENT_ID
  local envvar=$(echo "$container" |
    yq -r '.env[] | select(.name == "HCP_CLIENT_ID")' | tee /dev/stderr)

  local actual=$(echo "$envvar" |
    yq -r '.valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "client-id-name" ]

  actual=$(echo "$envvar" |
  yq -r '.valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "client-id-key" ]

  # HCP_CLIENT_SECRET
  envvar=$(echo "$container" |
  yq -r '.env[] | select(.name == "HCP_CLIENT_SECRET")' | tee /dev/stderr)

  local actual=$(echo "$envvar" |
    yq -r '.valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "client-secret-name" ]

  actual=$(echo "$envvar" |
  yq -r '.valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "client-secret-key" ]

  # HCP_RESOURCE_ID
  envvar=$(echo "$container" |
  yq -r '.env[] | select(.name == "HCP_RESOURCE_ID")' | tee /dev/stderr)

  local actual=$(echo "$envvar" |
    yq -r '.valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "resource-id-name" ]

  actual=$(echo "$envvar" |
  yq -r '.valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "resource-id-key" ]
}

@test "server/StatefulSet: creates HCP_AUTH_URL, HCP_SCADA_ADDRESS, and HCP_API_HOSTNAME envvars in consul container when global.cloud.enabled is true and those cloud values are specified" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml \
    --set 'global.cloud.enabled=true' \
    --set 'global.cloud.secretName=foo' \
    --set 'global.cloud.clientId.secretName=client-id-name' \
    --set 'global.cloud.clientId.secretKey=client-id-key' \
    --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
    --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
    --set 'global.cloud.resourceId.secretName=resource-id-name' \
    --set 'global.cloud.resourceId.secretKey=resource-id-key' \
    --set 'global.cloud.authUrl.secretName=auth-url-name' \
    --set 'global.cloud.authUrl.secretKey=auth-url-key' \
    --set 'global.cloud.apiHost.secretName=api-host-name' \
    --set 'global.cloud.apiHost.secretKey=api-host-key' \
    --set 'global.cloud.scadaAddress.secretName=scada-address-name' \
    --set 'global.cloud.scadaAddress.secretKey=scada-address-key' \
    . | tee /dev/stderr )

  local container=$(echo "$object" |
    yq -r '.spec.template.spec.containers[] | select(.name == "consul")' | tee /dev/stderr)

  # HCP_AUTH_URL
  envvar=$(echo "$container" |
  yq -r '.env[] | select(.name == "HCP_AUTH_URL")' | tee /dev/stderr)

  local actual=$(echo "$envvar" |
    yq -r '.valueFrom.secretKeyRef.name' | tee /dev/stderr)
  echo "actual: $actual"
  
  [ "${actual}" = "auth-url-name" ]

  actual=$(echo "$envvar" |
  yq -r '.valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "auth-url-key" ]

  # HCP_API_HOST
  envvar=$(echo "$container" |
  yq -r '.env[] | select(.name == "HCP_API_HOST")' | tee /dev/stderr)

  local actual=$(echo "$envvar" |
    yq -r '.valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "api-host-name" ]

  actual=$(echo "$envvar" |
  yq -r '.valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "api-host-key" ]

  # HCP_SCADA_ADDRESS
  envvar=$(echo "$container" |
  yq -r '.env[] | select(.name == "HCP_SCADA_ADDRESS")' | tee /dev/stderr)

  local actual=$(echo "$envvar" |
    yq -r '.valueFrom.secretKeyRef.name' | tee /dev/stderr)
  [ "${actual}" = "scada-address-name" ]

  actual=$(echo "$envvar" |
  yq -r '.valueFrom.secretKeyRef.key' | tee /dev/stderr)
  [ "${actual}" = "scada-address-key" ]
}

@test "server/StatefulSet: cloud config is set in command global.cloud.enabled is not set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.acls.enabled=true' \
      --set 'global.acls.bootstrapToken.secretName=name' \
      --set 'global.acls.bootstrapToken.secretKey=key' \
      . | tee /dev/stderr)

  # Test the flag is set.
  local actual=$(echo "$object" |
    yq '.spec.template.spec.containers[0].command | any(contains("-hcl=\"acl { tokens { initial_management = \\\"${ACL_BOOTSTRAP_TOKEN}\\\" } }\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  # Test the ACL_BOOTSTRAP_TOKEN environment variable is set.
  local actual=$(echo "$object" |
    yq -r -c '.spec.template.spec.containers[0].env | map(select(.name == "ACL_BOOTSTRAP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = '[{"name":"ACL_BOOTSTRAP_TOKEN","valueFrom":{"secretKeyRef":{"name":"name","key":"key"}}}]' ]
}

@test "server/StatefulSet: fails when global.cloud.enabled is true and global.cloud.clientId.secretName is not set but global.cloud.clientSecret.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientSecret.secretName=client-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=client-resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=client-resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When either global.cloud.resourceId.secretName or global.cloud.resourceId.secretKey is defined, both must be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.authUrl.secretName=auth-url-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.authUrl.secretName or global.cloud.authUrl.secretKey is defined, both must be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.authUrl.secretKey=auth-url-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.authUrl.secretName or global.cloud.authUrl.secretKey is defined, both must be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.apiHost.secretName=auth-url-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.apiHost.secretName or global.cloud.apiHost.secretKey is defined, both must be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.apiHost.secretKey=auth-url-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.apiHost.secretName or global.cloud.apiHost.secretKey is defined, both must be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.scadaAddress.secretName=scada-address-name' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.scadaAddress.secretName or global.cloud.scadaAddress.secretKey is defined, both must be set." ]]
}

@test "server/StatefulSet: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      --set 'global.cloud.scadaAddress.secretKey=scada-address-key' \
      .
  [ "$status" -eq 1 ]
  
  [[ "$output" =~ "When either global.cloud.scadaAddress.secretName or global.cloud.scadaAddress.secretKey is defined, both must be set." ]]
}

#--------------------------------------------------------------------
# server.snapshotAgent

@test "server/StatefulSet: snapshot-agent: snapshot agent container not added by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name == "consul-snapshot-agent")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}


@test "server/StatefulSet: snapshot-agent: snapshot agent container added with server.snapshotAGent.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[] | select(.name == "consul-snapshot-agent") | .name' | tee /dev/stderr)
  [ "${actual}" = "consul-snapshot-agent" ]
}

@test "server/StatefulSet: snapshot-agent: when server.snapshotAgent.configSecret.secretKey!=null and server.snapshotAgent.configSecret.secretName=null, fail" {
    cd `chart_dir`
    run helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.configSecret.secretName=' \
      --set 'server.snapshotAgent.configSecret.secretKey=bar' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "server.snapshotAgent.configSecret.secretKey and server.snapshotAgent.configSecret.secretName must both be specified." ]]
}

@test "server/StatefulSet: snapshot-agent: when server.snapshotAgent.configSecret.secretName!=null and server.snapshotAgent.configSecret.secretKey=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/server-statefulset.yaml  \
        --set 'server.snapshotAgent.enabled=true' \
        --set 'server.snapshotAgent.configSecret.secretName=foo' \
        --set 'server.snapshotAgent.configSecret.secretKey=' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "server.snapshotAgent.configSecret.secretKey and server.snapshotAgent.configSecret.secretName must both be specified." ]]
}

@test "server/StatefulSet: snapshot-agent: adds volume for snapshot agent config secret when secret is configured" {
  cd `chart_dir`
  local vol=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'server.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "snapshot-agent-user-config")' | tee /dev/stderr)
  local actual
  actual=$(echo $vol | jq -r '. .name' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-agent-user-config' ]

  actual=$(echo $vol | jq -r '. .secret.secretName' | tee /dev/stderr)
  [ "${actual}" = 'a/b/c/d' ]

  actual=$(echo $vol | jq -r '. .secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-agent-config' ]

  actual=$(echo $vol | jq -r '. .secret.items[0].path' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-config.json' ]
}

@test "server/StatefulSet: snapshot-agent: adds volume mount to snapshot container for snapshot agent config secret when secret is configured" {
  cd `chart_dir`
  local vol=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'server.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[1].volumeMounts[] | select(.name == "snapshot-agent-user-config")' | tee /dev/stderr)
  local actual
  actual=$(echo $vol | jq -r '. .name' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-agent-user-config' ]

  actual=$(echo $vol | jq -r '. .readOnly' | tee /dev/stderr)
  [ "${actual}" = 'true' ]

  actual=$(echo $vol | jq -r '. .mountPath' | tee /dev/stderr)
  [ "${actual}" = '/consul/user-config' ]
}

@test "server/StatefulSet: snapshot-agent: set config-dir argument on snapshot agent command to volume mount" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'server.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].command[2] | contains("-config-dir=/consul/user-config")' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

@test "server/StatefulSet: snapshot-agent: does not configure snapshot agent login config secret when acls are disabled" {
  cd `chart_dir`
  local spec=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=false' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec' | tee /dev/stderr)
  actual=$(echo $spec | yq -r '.volumes[] | select(.name == "snapshot-agent-config")')
  [ "${actual}" = "" ]

  actual=$(echo $spec | yq -r '.containers[1].volumeMounts')
  [ "${actual}" = "null" ]

  actual=$(echo $spec | yq -r '.containers[1].command[2] | contains("-config-file=/consul/config/snapshot-login.json")')
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: snapshot-agent: adds volume for snapshot agent login config secret when acls are enabled" {
  cd `chart_dir`
  local vol=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "snapshot-agent-config")' | tee /dev/stderr)
  local actual
  actual=$(echo $vol | jq -r '. .name' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-agent-config' ]

  actual=$(echo $vol | jq -r '. .configMap.name' | tee /dev/stderr)
  [ "${actual}" = 'release-name-consul-snapshot-agent-config' ]
}

@test "server/StatefulSet: snapshot-agent: adds volume mount to snapshot container for snapshot agent login config secret when acls are enabled" {
  cd `chart_dir`
  local vol=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[1].volumeMounts[] | select(.name == "snapshot-agent-config")' | tee /dev/stderr)
  local actual
  actual=$(echo $vol | jq -r '. .name' | tee /dev/stderr)
  [ "${actual}" = 'snapshot-agent-config' ]

  actual=$(echo $vol | jq -r '. .readOnly' | tee /dev/stderr)
  [ "${actual}" = 'true' ]

  actual=$(echo $vol | jq -r '. .mountPath' | tee /dev/stderr)
  [ "${actual}" = '/consul/config' ]
}

@test "server/StatefulSet: snapshot-agent: set config-file argument on snapshot agent command to login config when acls are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].command[2] | contains("-config-file=/consul/config/snapshot-login.json")' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

@test "server/StatefulSet: snapshot-agent: uses default consul addr when TLS is disabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/server-statefulset.yaml \
      --set 'server.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'http://127.0.0.1:8500' ]
}

@test "server/StatefulSet: snapshot-agent: sets TLS env vars when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/server-statefulset.yaml \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = 'https://127.0.0.1:8501' ]

  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "server/StatefulSet: snapshot-agent: populates container volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].volumeMounts[] | select(.name == "consul-ca-cert") | .name' | tee /dev/stderr)
  [ "${actual}" = "consul-ca-cert" ]
}

#--------------------------------------------------------------------
# server.snapshotAgent.resources

@test "server/StatefulSet: snapshot-agent: default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[1].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"50m","memory":"50Mi"},"requests":{"cpu":"50m","memory":"50Mi"}}' ]
}

@test "server/StatefulSet: snapshot-agent: can set resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.resources.requests.memory=100Mi' \
      --set 'server.snapshotAgent.resources.requests.cpu=100m' \
      --set 'server.snapshotAgent.resources.limits.memory=200Mi' \
      --set 'server.snapshotAgent.resources.limits.cpu=200m' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[1].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"200m","memory":"200Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}' ]
}

#--------------------------------------------------------------------
# server.snapshotAgent.caCert

@test "server/StatefulSet: snapshot-agent: if caCert is set command is modified correctly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.caCert=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].command[2] | contains("cat <<EOF > /extra-ssl-certs/custom-ca.pem")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: snapshot-agent: if caCert is set extra-ssl-certs volumeMount is added" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.caCert=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr | yq -r '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo $object | jq -r '.volumes[] | select(.name == "extra-ssl-certs") | .name' | tee /dev/stderr)
  [ "${actual}" = "extra-ssl-certs" ]
}

@test "server/StatefulSet: snapshot-agent: if caCert is set SSL_CERT_DIR env var is set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.caCert=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr | yq -r '.spec.template.spec.containers[1].env[] | select(.name == "SSL_CERT_DIR")' | tee /dev/stderr)

  local actual=$(echo $object | jq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "SSL_CERT_DIR" ]
  local actual=$(echo $object | jq -r '.value' | tee /dev/stderr)
  [ "${actual}" = "/etc/ssl/certs:/extra-ssl-certs" ]
}


#--------------------------------------------------------------------
# global.trustedCAs

@test "server/StatefulSet: trustedCAs: if trustedCAs is set command is modified correctly" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.trustedCAs[0]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command[2] | contains("cat <<EOF > /trusted-cas/custom-ca-0.pem")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: trustedCAs: if tustedCAs multiple are set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.trustedCAs[0]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      --set 'global.trustedCAs[1]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0]'  | tee /dev/stderr)


  local actual=$(echo $object | jq '.command[2] | contains("cat <<EOF > /trusted-cas/custom-ca-0.pem")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo $object | jq '.command[2] | contains("cat <<EOF > /trusted-cas/custom-ca-1.pem")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

# global.trustedCAs
@test "server/StatefulSet: trustedCAs: if trustedCAs is set /trusted-cas volumeMount is added" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.trustedCAs[0]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr | yq -r '.spec.template.spec' | tee /dev/stderr)
  local actual=$(echo $object | jq -r '.volumes[] | select(.name == "trusted-cas") | .name' | tee /dev/stderr)
  [ "${actual}" = "trusted-cas" ]
}

@test "server/StatefulSet: trustedCAs: if trustedCAs is set SSL_CERT_DIR env var is set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'global.trustedCAs[0]=-----BEGIN CERTIFICATE-----
MIICFjCCAZsCCQCdwLtdjbzlYzAKBggqhkjOPQQDAjB0MQswCQYDVQQGEwJDQTEL' \
      . | tee /dev/stderr | yq -r '.spec.template.spec.containers[0].env[] | select(.name == "SSL_CERT_DIR")' | tee /dev/stderr)

  local actual=$(echo $object | jq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "SSL_CERT_DIR" ]
  local actual=$(echo $object | jq -r '.value' | tee /dev/stderr)
  [ "${actual}" = "/etc/ssl/certs:/trusted-cas" ]
}

#--------------------------------------------------------------------
# snapshotAgent license-autoload

@test "server/StatefulSet: snapshot-agent: adds volume mount for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[1].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","mountPath":"/consul/license","readOnly":true}' ]
}

@test "server/StatefulSet: snapshot-agent: adds env var for license path when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[1].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"CONSUL_LICENSE_PATH","value":"/consul/license/bar"}' ]
}

@test "server/StatefulSet: snapshot-agent: does not add license secret volume mount if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[1].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "server/StatefulSet: snapshot-agent: does not add license env if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[1].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# snapshotAgent Vault

@test "server/StatefulSet: snapshot-agent: vault CONSUL_LICENSE_PATH is set to /vault/secrets/enterpriselicense.txt" {
  cd `chart_dir`
  local env=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'server.enabled=true' \
    --set 'server.snapshotAgent.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.enterpriseLicense.secretName=a/b/c/d' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
    . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].env[]' | tee /dev/stderr)

  local actual

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_LICENSE_PATH") | .value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/enterpriselicense.txt" ]
}

@test "server/StatefulSet: snapshot-agent: vault does not add volume mount for license secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.enterpriseLicense.secretName=a/b/c/d' \
      --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[1].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "server/StatefulSet: snapshot-agent: vault snapshot agent config annotations are correct when enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-statefulset.yaml  \
    --set 'server.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'server.snapshotAgent.enabled=true' \
    --set 'server.snapshotAgent.configSecret.secretName=path/to/secret' \
    --set 'server.snapshotAgent.configSecret.secretKey=config' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-snapshot-agent-config.json"]' | tee /dev/stderr)
  [ "${actual}" = "path/to/secret" ]

  actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-snapshot-agent-config.json"]' | tee /dev/stderr)
  local expected=$'{{- with secret \"path/to/secret\" -}}\n{{- .Data.data.config -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  actual=$(echo $object | jq -r '.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "test" ]
}

@test "server/StatefulSet: snapshot-agent: vault does not add volume for snapshot agent config secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'server.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "snapshot-agent-user-config")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "server/StatefulSet: snapshot-agent: vault does not add volume mount for snapshot agent config secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'server.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "snapshot-agent-user-config")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "server/StatefulSet: snapshot-agent: vault sets config-file argument on snapshot agent command to config downloaded by vault agent injector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.configSecret.secretName=a/b/c/d' \
      --set 'server.snapshotAgent.configSecret.secretKey=snapshot-agent-config' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].command[2] | contains("-config-file=/vault/secrets/snapshot-agent-config.json")' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

#--------------------------------------------------------------------
# snapshotAgent Interval

@test "server/StatefulSet: snapshot-agent: interval defaults to 1h" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'server.snapshotAgent.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].command[2] | contains("-interval=1h")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: snapshot-agent: interval can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml  \
      --set 'server.enabled=true' \
      --set 'server.snapshotAgent.enabled=true' \
      --set 'server.snapshotAgent.interval=10h34m5s' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[1].command[2] | contains("-interval=10h34m5s")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

