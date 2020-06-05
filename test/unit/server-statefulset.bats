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
# retry-join

@test "server/StatefulSet: retry join gets populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | any(contains("-retry-join"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
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

@test "server/StatefulSet: resources defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"100m","memory":"100Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}' ]
}

@test "server/StatefulSet: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.resources.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

# Test support for the deprecated method of setting a YAML string.
@test "server/StatefulSet: resources can be overridden with string" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'server.resources=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
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

@test "server/StatefulSet: adds extra secret volume with items" {
  cd `chart_dir`

  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
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

#--------------------------------------------------------------------
# global.tls.enabled

@test "server/StatefulSet: CA volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: server volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-server-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: CA volume mounted when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: server certificate volume mounted when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-server-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: port 8501 is not exposed when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8501)' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "server/StatefulSet: port 8501 is exposed when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8501)' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: port 8500 is still exposed when httpsOnly is not enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8500)' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "server/StatefulSet: port 8500 is not exposed when httpsOnly is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8500)' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "server/StatefulSet: readiness checks are over HTTP when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("http://127.0.0.1:8500")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: readiness checks are over HTTPS when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("https://127.0.0.1:8501")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: CA certificate is specified when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("--cacert /consul/tls/ca/tls.crt")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: HTTP is disabled in agent when httpsOnly is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("ports { http = -1 }")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: sets Consul environment variables when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "https://localhost:8501" ]

  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
    [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "server/StatefulSet: sets verify_* flags to true by default when global.tls.enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ")' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | contains("verify_incoming_rpc = true")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | contains("verify_outgoing = true")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | contains("verify_server_hostname = true")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: doesn't set the verify_* flags by default when global.tls.enabled and global.tls.verify is false" {
  cd `chart_dir`
  local command=$(helm template \
      -x templates/server-statefulset.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.verify=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ")' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | contains("verify_incoming_rpc = true")' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  actual=$(echo $command | jq -r '. | contains("verify_outgoing = true")' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  actual=$(echo $command | jq -r '. | contains("verify_server_hostname = true")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/StatefulSet: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -x templates/server-statefulset.yaml \
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
      -x templates/server-statefulset.yaml  \
      --set 'global.federation.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.federation.enabled is true, global.tls.enabled must be true because federation is only supported with TLS enabled" ]]
}

@test "server/StatefulSet: mesh gateway federation enabled when federation.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("connect { enable_mesh_gateway_wan_federation = true }")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/StatefulSet: mesh gateway federation not enabled when federation.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.federation.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("connect { enable_mesh_gateway_wan_federation = true }")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# global.acls.replicationToken

@test "server/StatefulSet: acl replication token config is not set by default" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-statefulset.yaml  \
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
      -x templates/server-statefulset.yaml  \
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
      -x templates/server-statefulset.yaml  \
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
      -x templates/server-statefulset.yaml  \
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
# global.tls.enableAutoEncrypt

@test "server/StatefulSet: enables auto-encrypt for the servers when global.tls.enableAutoEncrypt is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-statefulset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("auto_encrypt = {allow_tls = true}")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
