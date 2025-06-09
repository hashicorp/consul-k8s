#!/usr/bin/env bats

load _helpers

@test "client/DaemonSet: enabled with global.enabled=false and client.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: disabled with client.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=false' \
      .
}

@test "client/DaemonSet: disabled with global.enabled=false and client.enabled='-'" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "client/DaemonSet: image defaults to global.image" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.image=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

@test "client/DaemonSet: image can be overridden with client.image" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.image=foo' \
      --set 'client.image=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "client/DaemonSet: no updateStrategy when not updating" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.updateStrategy' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

#--------------------------------------------------------------------
# retry-join

@test "client/DaemonSet: retry join gets populated by default" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8301\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"${CONSUL_FULLNAME}-server-1.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8301\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"${CONSUL_FULLNAME}-server-2.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8301\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: retry join uses the server.ports.serflan port" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'server.replicas=3' \
      --set 'server.ports.serflan.port=9301' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:9301\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"${CONSUL_FULLNAME}-server-1.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:9301\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"${CONSUL_FULLNAME}-server-2.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:9301\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: retry join gets populated when client.join is set" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'client.join[0]=1.1.1.1' \
      --set 'client.join[1]=2.2.2.2' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command')

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"1.1.1.1\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"2.2.2.2\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: can provide cloud auto-join string to client.join" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo' \
      --set 'client.join[0]=provider=my-cloud config=val' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command')

  local actual=$(echo $command | jq -r ' . | any(contains("-retry-join=\"provider=my-cloud config=val\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# grpc

@test "client/DaemonSet: grpc is enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("grpc"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: grpc can be disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.grpc=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("grpc"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# nodeMeta

@test "client/DaemonSet: meta-data pod-name:\${HOSTNAME} by default at nodeMeta" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-node-meta=pod-name:${HOSTNAME}"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: meta-data host-ip: \${HOST_IP} by default at nodeMeta" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-node-meta=host-ip:${HOST_IP}"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: pod-name can be configured at nodeMeta" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.nodeMeta.pod-name=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-node-meta=pod-name:foobar"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: additional meta-data at nodeMeta" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.nodeMeta.cluster-name=cluster01' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-node-meta=cluster-name:cluster01"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# resources

@test "client/DaemonSet: resources defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"100m","memory":"100Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}' ]
}

@test "client/DaemonSet: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.resources.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

# Test support for the deprecated method of setting a YAML string.
@test "client/DaemonSet: resources can be overridden with string" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.resources=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# extra-config

@test "client/DaemonSet: has extra-config volume" {
  cd `chart_dir`

  # check that the extra-config volume is defined
  local volume_name=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[] | select(.name == "extra-config") | .name' | tee /dev/stderr)
  [ "${volume_name}" = "extra-config" ]

  # check that the consul container mounts the volume at /consul/extra-config
  local mount_path=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[] | select(.name == "consul") | .volumeMounts[] | select(.name == "extra-config") | .mountPath' | tee /dev/stderr)
  [ "${mount_path}" = "/consul/extra-config" ]
}

#--------------------------------------------------------------------
# extraVolumes

@test "client/DaemonSet: adds extra volume" {
  cd `chart_dir`

  # Test that it defines it
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraVolumes[0].type=configMap' \
      --set 'client.extraVolumes[0].name=foo' \
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
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraVolumes[0].type=configMap' \
      --set 'client.extraVolumes[0].name=foo' \
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
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraVolumes[0].type=configMap' \
      --set 'client.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | map(select(test("userconfig"))) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "client/DaemonSet: adds extra secret volume" {
  cd `chart_dir`

  # Test that it defines it
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraVolumes[0].type=secret' \
      --set 'client.extraVolumes[0].name=foo' \
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
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraVolumes[0].type=configMap' \
      --set 'client.extraVolumes[0].name=foo' \
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
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraVolumes[0].type=configMap' \
      --set 'client.extraVolumes[0].name=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | map(select(test("userconfig"))) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "client/DaemonSet: adds loadable volume" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraVolumes[0].type=configMap' \
      --set 'client.extraVolumes[0].name=foo' \
      --set 'client.extraVolumes[0].load=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].command | map(select(contains("/consul/userconfig/foo"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "client/DaemonSet: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/DaemonSet: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      --set 'client.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# affinity

@test "client/DaemonSet: affinity not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .affinity? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: specified affinity" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.affinity=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .affinity == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "client/DaemonSet: priorityClassName is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/DaemonSet: specified priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.priorityClassName=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "client/DaemonSet: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component") | del(."hasDNS")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "client/DaemonSet: extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraLabels.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

@test "client/DaemonSet: multiple extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraLabels.foo=bar' \
      --set 'client.extraLabels.baz=qux' \
      . | tee /dev/stderr)
  local actualFoo=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  local actualBaz=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualFoo}" = "bar" ]
  [ "${actualBaz}" = "qux" ]
}

@test "client/DaemonSet: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "client/DaemonSet: multiple extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
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

@test "client/DaemonSet: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."consul.hashicorp.com/config-checksum")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "client/DaemonSet: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# metrics

@test "client/DaemonSet: when global.metrics.enableAgentMetrics=true, adds prometheus scrape=true annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/scrape"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: when global.metrics.enableAgentMetrics=true, adds prometheus port=8500 annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/port"' | tee /dev/stderr)
  [ "${actual}" = "8500" ]
}

@test "client/DaemonSet: when global.metrics.enableAgentMetrics=true, adds prometheus path=/v1/agent/metrics annotation" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/v1/agent/metrics" ]
}

@test "client/DaemonSet: when global.metrics.enableAgentMetrics=true, and client annotation for prometheus path is specified, it uses the client annotation rather than default." {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'client.annotations=prometheus.io/path: /anew/path' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."prometheus.io/path"' | tee /dev/stderr)
  [ "${actual}" = "/anew/path" ]
}

@test "client/DaemonSet: when global.metrics.enableAgentMetrics=true, sets telemetry flag" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("telemetry { prometheus_retention_time = \"1m\" }")' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: when global.metrics.enableAgentMetrics=true and global.metrics.agentMetricsRetentionTime is set, sets telemetry flag with updated retention time" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.agentMetricsRetentionTime=5m'  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("telemetry { prometheus_retention_time = \"5m\" }")' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# config-configmap

@test "client/DaemonSet: config-checksum annotation when extraConfig is blank" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  [ "${actual}" = 678c5c1c2ca0f8cb1464d38636f12714c05df26fab1a101e43ce619fdbc2e7d1 ]
}

@test "client/DaemonSet: config-checksum annotation changes when extraConfig is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  [ "${actual}" = 0ef58da6fd14fb57c702a2a0d631c4eecacff152fe3a36836a23283b19d8dbe1 ]
}

@test "client/DaemonSet: config-checksum annotation changes when connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  [ "${actual}" = 678c5c1c2ca0f8cb1464d38636f12714c05df26fab1a101e43ce619fdbc2e7d1 ]
}

#--------------------------------------------------------------------
# tolerations

@test "client/DaemonSet: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .tolerations? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.tolerations=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# gossip encryption

@test "client/DaemonSet: gossip encryption disabled in client DaemonSet by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "client/DaemonSet: gossip encryption autogeneration properly sets secretName and secretKey" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.gossipEncryption.autoGenerate=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | .valueFrom.secretKeyRef | [.name=="release-name-consul-gossip-encryption-key", .key="key"] | all' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: gossip encryption key is passed in via the -encrypt flag" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.gossipEncryption.autoGenerate=true' \
    . | tee /dev/stderr |
    yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | any(contains("-encrypt=\"${GOSSIP_KEY}\""))' \
    | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: gossip encryption disabled in client DaemonSet when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.gossipEncryption.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "client/DaemonSet: gossip encryption disabled in client DaemonSet when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.gossipEncryption.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "client/DaemonSet: gossip environment variable present in client DaemonSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: encrypt CLI option not present in client DaemonSet when encryption disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | join(" ") | contains("encrypt")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: encrypt CLI option present in client DaemonSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.gossipEncryption.secretKey=foo' \
      --set 'global.gossipEncryption.secretName=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | join(" ") | contains("encrypt")' | tee /dev/stderr)
  [ "${actual}" == "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "client/DaemonSet: CA cert volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: CA key volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-key")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: client certificate volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-client-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: port 8501 is not exposed when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8501)' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "client/DaemonSet: port 8501 is exposed when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8501)' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: port 8500 is still exposed when httpsOnly is not enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8500)' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: port 8500 is not exposed when httpsOnly is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8500)' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "client/DaemonSet: readiness checks are over HTTP TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("http://127.0.0.1:8500")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: readiness checks are over HTTPS when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("https://127.0.0.1:8501")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: readiness checks skip TLS verification when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("-k")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: HTTP port is disabled when global.tls.httpsOnly is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("ports { http = -1 }")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: TLS GRPC port is configured" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("ports { grpc = -1, grpc_tls = 8502 }")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: non-TLS GRPC port is configured when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("ports { grpc = 8502, grpc_tls = -1 }")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: init container is created when global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-tls-init") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: Adds consul envvars CONSUL_HTTP_ADDR on acl-init init container when ACLs are enabled and tls is enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_ADDRESSES") | .value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-server.default.svc" ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_PORT") | .value' | tee /dev/stderr)
  [ "${actual}" = "8501" ]
}

@test "client/DaemonSet: Adds consul envvars CONSUL_HTTP_ADDR on acl-init init container when ACLs are enabled and tls is not enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_ADDRESSES") | .value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-server.default.svc" ]

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_PORT") | .value' | tee /dev/stderr)
  [ "${actual}" = "8500" ]
}

@test "client/DaemonSet: Does not add consul envvars CONSUL_CACERT on acl-init init container when ACLs are enabled and tls is not enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0].env[] | select(.name == "CONSUL_CACERT")' | tee /dev/stderr)

  [ "${actual}" = "" ]
}

@test "client/DaemonSet: Adds consul envvars CONSUL_CACERT on acl-init init container when ACLs are enabled and tls is enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT_FILE") | .value' | tee /dev/stderr)
    [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "client/DaemonSet: both ACL and TLS init containers are created when global.tls.enabled=true and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local has_acl_init_container=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | length > 0' | tee /dev/stderr)

  [ "${has_acl_init_container}" = "true" ]

  local has_tls_init_container=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | length > 0' | tee /dev/stderr)

  [ "${has_tls_init_container}" = "true" ]
}

@test "client/DaemonSet: sets Consul environment variables when global.tls.enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual
  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_HTTP_ADDR") | .value' | tee /dev/stderr)
  [ "${actual}" = "https://localhost:8501" ]

  actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT") | .value' | tee /dev/stderr)
    [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

@test "client/DaemonSet: sets verify_* flags to true by default when global.tls.enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
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

@test "client/DaemonSet: sets verify_* flags to true by default when global.tls.enabled and global.peering.enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.peering.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ")' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | contains("tls { internal_rpc { verify_incoming = true }}")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | contains("tls { defaults { verify_outgoing = true }}")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | contains("tls { internal_rpc { verify_server_hostname = true }}")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: sets updated TLS config when global.tls.enabled and global.peering.enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.peering.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ")' | tee /dev/stderr)

  local actual
  actual=$(echo $command | jq -r '. | contains("tls { defaults { ca_file = \"/consul/tls/ca/tls.crt\" }}")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | contains("tls { defaults { cert_file = \"/consul/tls/client/tls.crt\" }}")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $command | jq -r '. | contains("tls { defaults { key_file = \"/consul/tls/client/tls.key\" }}")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: doesn't set the verify_* flags when global.tls.enabled is true and global.tls.verify is false" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
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

@test "client/DaemonSet: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local spec=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo-ca-cert' \
      --set 'global.tls.caCert.secretKey=key' \
      --set 'global.tls.caKey.secretName=foo-ca-key' \
      --set 'global.tls.caKey.secretKey=key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec' | tee /dev/stderr)

  # check that the provided ca cert secret is attached as a volume
  local actual
  actual=$(echo $spec | jq -r '.volumes[] | select(.name=="consul-ca-cert") | .secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-cert" ]

  # check that the provided ca key secret is attached as volume
  actual=$(echo $spec | jq -r '.volumes[] | select(.name=="consul-ca-key") | .secret.secretName' | tee /dev/stderr)
  [ "${actual}" = "foo-ca-key" ]

  # check that the volumes pulls the provided secret keys as a CA cert
  actual=$(echo $spec | jq -r '.volumes[] | select(.name=="consul-ca-cert") | .secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]

  # check that the volumes pulls the provided secret keys as a CA key
  actual=$(echo $spec | jq -r '.volumes[] | select(.name=="consul-ca-key") | .secret.items[0].key' | tee /dev/stderr)
  [ "${actual}" = "key" ]
}

#--------------------------------------------------------------------
# global.tls.enableAutoEncrypt

@test "client/DaemonSet: client certificate volume is not present when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-client-cert")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "client/DaemonSet: sets auto_encrypt options for the client if auto-encrypt is enabled" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ")' | tee /dev/stderr)

  # enables auto encrypt on the client
  actual=$(echo $command | jq -r '. | contains("auto_encrypt = {tls = true}")' | tee /dev/stderr)
  [ "${actual}" == "true" ]

  # sets IP SANs to contain the HOST IP of the client
  actual=$(echo $command | jq -r '. | contains("auto_encrypt = {ip_san = [\\\"$HOST_IP\\\",\\\"$POD_IP\\\"]}")' | tee /dev/stderr)
  [ "${actual}" == "true" ]

  # doesn't set verify_incoming_rpc and verify_server_hostname
  actual=$(echo $command | jq -r '. | contains("verify_incoming_rpc = true")' | tee /dev/stderr)
  [ "${actual}" == "false" ]

  actual=$(echo $command | jq -r '. | contains("verify_server_hostname = true")' | tee /dev/stderr)
  [ "${actual}" == "false" ]
}

@test "client/DaemonSet: init container is not created when global.tls.enabled=true and global.tls.enableAutoEncrypt=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: CA key volume is not present when TLS is enabled and global.tls.enableAutoEncrypt=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-key")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "client/DaemonSet: client certificate volume is not present when TLS is enabled and global.tls.enableAutoEncrypt=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-client-cert")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "client/DaemonSet: sets CONSUL_HTTP_SSL_VERIFY environment variable to false when global.tls.enabled and global.tls.enableAutoEncrypt=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_HTTP_SSL_VERIFY") | .value' | tee /dev/stderr)
  [ "${actual}" == "false" ]
}

#--------------------------------------------------------------------
# extraEnvironmentVariables

@test "client/DaemonSet: custom environment variables" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraEnvironmentVars.custom_proxy=fakeproxy' \
      --set 'client.extraEnvironmentVars.no_proxy=custom_no_proxy' \
      --set "client.extraEnvironmentVars.from_annotations.fieldRef.fieldPath=metadata.annotations['foo']" \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.[] | select(.name=="custom_proxy").value' | tee /dev/stderr)
  [ "${actual}" = "fakeproxy" ]

  local actual=$(echo $object |
      yq -r '.[] | select(.name=="no_proxy").value' | tee /dev/stderr)
  [ "${actual}" = "custom_no_proxy" ]

  local actual=$(echo $object |
      yq -r '.[] | select(.name=="from_annotations").valueFrom.fieldRef.fieldPath' | tee /dev/stderr)
  [ "${actual}" = "metadata.annotations['foo']" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "client/DaemonSet: aclconfig volume is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local volume_name=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[] | select(.name == "aclconfig") | .name' | tee /dev/stderr)
  [ "${volume_name}" = "aclconfig" ]
}

@test "client/DaemonSet: aclconfig volumeMount is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local mount_path=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[] | select(.name == "consul") | .volumeMounts[] | select(.name == "aclconfig") | .mountPath' | tee /dev/stderr)
  [ "${mount_path}" = "/consul/aclconfig" ]
}

@test "client/DaemonSet: command includes aclconfig dir when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("/consul/aclconfig"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: init container is created when global.acls.manageSystemACLs=true and command args are properly set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("secret-name"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
      yq -r '.env[2].name | contains("CONSUL_ADDRESSES")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[2].value | contains("release-name-consul-server.default.svc")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[7].name | contains("CONSUL_LOGIN_AUTH_METHOD")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[7].value | contains("release-name-consul-k8s-component-auth-method")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[8].name | contains("CONSUL_LOGIN_META")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[8].value | contains("component=client")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("init-type=\"client\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]  

  local actual=$(echo $object |
      yq -r '.command | any(contains("log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]  

  local actual=$(echo $object |
      yq -r '.command | any(contains("log-json=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]  
}

@test "client/DaemonSet: init container is created when global.acls.manageSystemACLs=true and has correct command with Partitions enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=default' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("secret-name"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]  

  local actual=$(echo $object |
      yq -r '.env[2].name | contains("CONSUL_ADDRESSES")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[2].value | contains("release-name-consul-server.default.svc")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[7].name | contains("CONSUL_PARTITION")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[7].value | contains("default")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[8].name | contains("CONSUL_LOGIN_PARTITION")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[8].value | contains("default")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[9].name | contains("CONSUL_LOGIN_AUTH_METHOD")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[9].value | contains("release-name-consul-k8s-component-auth-method")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[10].name | contains("CONSUL_LOGIN_META")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[10].value | contains("component=client")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("init-type=\"client\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]  

  local actual=$(echo $object |
      yq -r '.command | any(contains("log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]  

  local actual=$(echo $object |
      yq -r '.command | any(contains("log-json=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: CONSUL_HTTP_TOKEN_FILE is not set when acls are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=false' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[] | select(.name == "consul") | .env[] | .name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: CONSUL_HTTP_TOKEN_FILE is set when acls are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[] | select(.name == "consul") | .env[] | .name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: consul-logout preStop hook is added when ACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].lifecycle.preStop.exec.command[1]] | any(contains("logout"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: Adds consul login volume when ACLs are enabled" {
  cd `chart_dir`
  local volume=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | yq '.spec.template.spec.volumes[] | select(.name == "consul-data")' | tee /dev/stderr)

  local volume_name=$(echo $volume |
      yq -r '.name' | tee /dev/stderr)
  [ "${volume_name}" = "consul-data" ]

  local volume_emptydir_medium=$(echo $volume |
      yq -r '.emptyDir.medium' | tee /dev/stderr)
  [ "${volume_emptydir_medium}" = "Memory" ]
}

@test "client/DaemonSet: Adds consul login volumeMount to client container when ACLs are enabled" {
  cd `chart_dir`
  local volume_mount=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | yq '.spec.template.spec.containers[] | select(.name == "consul") | .volumeMounts[] | select(.name == "consul-data")' | tee /dev/stderr)

  local volume_mount_path=$(echo $volume_mount |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${volume_mount_path}" = "/consul/login" ]

  local volume_mount_ro=$(echo $volume_mount |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${volume_mount_ro}" = "true" ]
}

@test "client/DaemonSet: Adds consul login volumeMount to acl-init init container when ACLs are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | yq '.spec.template.spec.initContainers[0].volumeMounts[1]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "consul-data" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/login" ]

  local actual=$(echo $object |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: Adds consul ca cert volumeMount to acl-init init container when ACLs and tls are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      . | yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | .volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "consul-ca-cert" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca" ]

  local actual=$(echo $object |
      yq -r '.readOnly' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: Does not add consul ca cert volumeMount to acl-init init container when tls is not enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=false' \
      . | yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | .volumeMounts[] | select(.name=="consul-ca-cert")' | tee /dev/stderr)
  [ "${object}" == "" ]
}

@test "client/DaemonSet: fail when externalServers is enabled but the externalServers.hosts is not provided" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=true'  \
      --set 'server.enabled=false' \
      .
  echo "status:$status"
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.hosts must be set if externalServers.enabled is true" ]]
}

@test "client/DaemonSet: addresses env is set with hosts when externalServers.hosts are provided" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=true'  \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=foo'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.env[2].name | contains("CONSUL_ADDRESSES")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[2].value | contains("foo")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: tls-server-name flag is set when externalServers.tlsServerName is provided" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true'  \
      --set 'externalServers.enabled=true'  \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=computer'  \
      --set 'externalServers.tlsServerName=foo'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.env[2].name | contains("CONSUL_ADDRESSES")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[2].value | contains("computer")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[9].name | contains("CONSUL_TLS_SERVER_NAME")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[9].value | contains("foo")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: tls-server-name flag is not set when externalServers.tlsServerName is not provided" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=true'  \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=computer'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | .command' | tee /dev/stderr)

  local actual=$(echo $command | jq -r ' . | any(contains("-tls-server-name"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: use-https flag is not set when global.tls.enabled is not provided" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=true'  \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=computer'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | .command' | tee /dev/stderr)

  local actual=$(echo $command | jq -r ' . | any(contains("-use-https"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: use-tls env is set when global.tls.enabled is provided and externalServers.enabled is true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=true'  \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=computer'  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.env[7].name | contains("CONSUL_USE_TLS")' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.env[7].value | contains("true")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: use-https flag is not set when global.tls.enabled is enabled but externalServers.enabled is false" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=false'  \
      --set 'server.enabled=false' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.hosts[0]=computer'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | .command' | tee /dev/stderr)

  local actual=$(echo $command | jq -r ' . | any(contains("-use-https"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: server-port flag is not set when externalServers.enabled is false" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=false'  \
      --set 'server.enabled=false' \
      --set 'externalServers.hosts[0]=computer'  \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | .command' | tee /dev/stderr)

  local actual=$(echo $command | jq -r ' . | any(contains("-server-port"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# client.exposeGossipPorts

@test "client/DaemonSet: client uses podIP when client.exposeGossipPorts=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.enabled=true' \
      --set 'client.exposeGossipPorts=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name=="ADVERTISE_IP") | .valueFrom.fieldRef.fieldPath' |
      tee /dev/stderr)
  [ "${actual}" = "status.podIP" ]
}

@test "client/DaemonSet: client uses hostIP when client.exposeGossipPorts=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.enabled=true' \
      --set 'client.exposeGossipPorts=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name=="ADVERTISE_IP") | .valueFrom.fieldRef.fieldPath' |
      tee /dev/stderr)
  [ "${actual}" = "status.hostIP" ]
}

@test "client/DaemonSet: client doesn't expose hostPorts when client.exposeGossipPorts=false" {
  cd `chart_dir`
  local has_exposed_host_ports=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'server.enabled=true' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[] | select(.name=="consul") | .ports[] | select(.containerPort==8301)] | any(has("hostPort"))' |
      tee /dev/stderr)
  [ "${has_exposed_host_ports}" = "false" ]
}

@test "client/DaemonSet: client exposes hostPorts when client.exposeGossipPorts=true" {
  cd `chart_dir`
  local has_exposed_host_ports=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.enabled=true' \
      --set 'client.exposeGossipPorts=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[] | select(.name=="consul") | .ports[] | select(.containerPort==8301)] | all(has("hostPort"))' |
      tee /dev/stderr)
  [ "${has_exposed_host_ports}" = "true" ]
}

#--------------------------------------------------------------------
# dataDirectoryHostPath

@test "client/DaemonSet: data directory is emptyDir by defaut" {
  cd `chart_dir`
  # Test that hostPath is set to null.
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "data") | .hostPath == null' | tee /dev/stderr )
  [ "${actual}" = "true" ]

  # Test that emptyDir is set instead.
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "data") | .emptyDir == {}' | tee /dev/stderr )
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: hostPath data directory can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.dataDirectoryHostPath=/opt/consul' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "data") | .hostPath.path == "/opt/consul"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# dnsPolicy

@test "client/DaemonSet: dnsPolicy not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.dnsPolicy == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: dnsPolicy can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      --set 'client.dnsPolicy=ClusterFirstWithHostNet' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.dnsPolicy == "ClusterFirstWithHostNet"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# hostNetwork

@test "client/DaemonSet: hostNetwork not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.hostNetwork == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: hostNetwork can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      --set 'client.hostNetwork=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.hostNetwork == true' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
#--------------------------------------------------------------------
# updateStrategy

@test "client/DaemonSet: updateStrategy not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      . | tee /dev/stderr | \
      yq '.spec.updateStrategy == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: updateStrategy can be set" {
  cd `chart_dir`
  local updateStrategy="type: RollingUpdate
rollingUpdate:
  maxUnavailable: 5
"
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
      --set 'client.enabled=true' \
      --set "client.updateStrategy=${updateStrategy}" \
      . | tee /dev/stderr | \
      yq -c '.spec.updateStrategy == {"type":"RollingUpdate","rollingUpdate":{"maxUnavailable":5}}' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.openshift.enabled & client.securityContext

@test "client/DaemonSet: securityContext is not set when global.openshift.enabled=true" {
  cd `chart_dir`
  local has_security_context=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec | has("securityContext")' | tee /dev/stderr)
  [ "${has_security_context}" = "false" ]
}

#--------------------------------------------------------------------
# client.securityContext

@test "client/DaemonSet: sets default security context settings" {
  cd `chart_dir`
  local security_context=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)

  local actual=$(echo $security_context | jq -r .runAsNonRoot)
  [ "${actual}" = "true" ]

  local actual=$(echo $security_context | jq -r .fsGroup)
  [ "${actual}" = "1000" ]

  local actual=$(echo $security_context | jq -r .runAsUser)
  [ "${actual}" = "100" ]

  local actual=$(echo $security_context | jq -r .runAsGroup)
  [ "${actual}" = "1000" ]
}

@test "client/DaemonSet: can overwrite security context settings" {
  cd `chart_dir`
  local security_context=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.securityContext.runAsNonRoot=false' \
      --set 'client.securityContext.privileged=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)

  local actual=$(echo $security_context | jq -r .runAsNonRoot)
  [ "${actual}" = "false" ]

  local actual=$(echo $security_context | jq -r .privileged)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# client.containerSecurityContext.*

@test "client/DaemonSet: Can set container level securityContexts" {
  cd `chart_dir`
  local manifest=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=false' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.containerSecurityContext.client.privileged=false' \
      --set 'client.containerSecurityContext.aclInit.allowPrivilegeEscalation=false' \
      --set 'client.containerSecurityContext.tlsInit.readOnlyRootFileSystem=true' \
      . | tee /dev/stderr)

  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.containers[] | select(.name == "consul") | .securityContext.privileged')
  [ "${actual}" = "false" ]

  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | .securityContext.allowPrivilegeEscalation')
  [ "${actual}" = "false" ]

  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.initContainers[] | select(.name == "client-tls-init") | .securityContext.readOnlyRootFileSystem')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.openshift.enabled & client.containerSecurityContext

@test "client/DaemonSet: container level securityContexts are not set when global.openshift.enabled=true" {
  cd `chart_dir`
  local manifest=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.openshift.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=false' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.containerSecurityContext.client.privileged=false' \
      --set 'client.containerSecurityContext.aclInit.allowPrivilegeEscalation=false' \
      --set 'client.containerSecurityContext.tlsInit.readOnlyRootFileSystem=true' \
      . | tee /dev/stderr)

  local has_security_context=$(echo "$manifest" | yq -r '.spec.template.spec.containers[] | select(.name == "consul") | has("securityContext")')
  [ "${has_security_context}" = "false" ]

  local has_security_context=$(echo "$manifest" | yq -r '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | has("securityContext")')
  [ "${has_security_context}" = "false" ]

  local has_security_context=$(echo "$manifest" | yq -r '.spec.template.spec.initContainers[] | select(.name == "client-tls-init") | has("securityContext")')
  [ "${has_security_context}" = "false" ]
}

#--------------------------------------------------------------------
# license-autoload

@test "client/DaemonSet: adds volume for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","secret":{"secretName":"foo"}}' ]
}

@test "client/DaemonSet: adds volume mount for license secret when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"consul-license","mountPath":"/consul/license","readOnly":true}' ]
}

@test "client/DaemonSet: adds env var for license path when enterprise license secret name and key are provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"CONSUL_LICENSE_PATH","value":"/consul/license/bar"}' ]
}

@test "client/DaemonSet: does not add license secret volume if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/DaemonSet: does not add license secret volume mount if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/DaemonSet: does not add license env if manageSystemACLs are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_LICENSE_PATH")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/DaemonSet: when global.enterpriseLicense.secretKey!=null and global.enterpriseLicense.secretName=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/client-daemonset.yaml \
        --set 'client.enabled=true' \
        --set 'global.enterpriseLicense.secretName=' \
        --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "enterpriseLicense.secretKey and secretName must both be specified." ]]
}

@test "client/DaemonSet: when global.enterpriseLicense.secretName!=null and global.enterpriseLicense.secretKey=null, fail" {
    cd `chart_dir`
    run helm template \
        -s templates/client-daemonset.yaml \
        --set 'client.enabled=true' \
        --set 'global.enterpriseLicense.secretName=foo' \
        --set 'global.enterpriseLicense.secretKey=' \
        .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "enterpriseLicense.secretKey and secretName must both be specified." ]]
}
#--------------------------------------------------------------------
# recursors

@test "client/DaemonSet: -recursor can be set by global.recursors" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.recursors[0]=1.2.3.4' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-recursor=\"1.2.3.4\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
#--------------------------------------------------------------------
# partitions

@test "client/DaemonSet: -partitions can be set by global.adminPartitions.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("partition = \"default\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: -partitions can be overridden by global.adminPartitions.name" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=test' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=bar' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("partition = \"test\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: partition name has to be default in server cluster" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=test' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.adminPartitions.name has to be \"default\" in the server cluster" ]]
}

@test "client/DaemonSet: federation and admin partitions cannot be enabled together" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.federation.enabled=true' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "If global.federation.enabled is true, global.adminPartitions.enabled must be false because they are mutually exclusive" ]]
}

#--------------------------------------------------------------------
# extraContainers

@test "client/DaemonSet: extraContainers adds extra container" {
  cd `chart_dir`

  # Test that it defines the extra container
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraContainers[0].image=test-image' \
      --set 'client.extraContainers[0].name=test-container' \
      --set 'client.extraContainers[0].ports[0].name=test-port' \
      --set 'client.extraContainers[0].ports[0].containerPort=9410' \
      --set 'client.extraContainers[0].ports[0].protocol=TCP' \
      --set 'client.extraContainers[0].env[0].name=TEST_ENV' \
      --set 'client.extraContainers[0].env[0].value=test_env_value' \
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

@test "client/DaemonSet: extraContainers supports adding two containers" {
  cd `chart_dir`

  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraContainers[0].image=test-image' \
      --set 'client.extraContainers[0].name=test-container' \
      --set 'client.extraContainers[1].image=test-image' \
      --set 'client.extraContainers[1].name=test-container-2' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers | length' | tee /dev/stderr)

  [ "${object}" = 3 ]

}

@test "client/DaemonSet: no extra client containers added by default" {
  cd `chart_dir`

  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers | length' | tee /dev/stderr)

  [ "${object}" = 1 ]
}

#--------------------------------------------------------------------
# vault integration

@test "client/DaemonSet: fail when vault is enabled but the consulClientRole is not provided" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.secretsBackend.vault.consulClientRole must be provided if global.secretsBackend.vault.enabled=true" ]]
}

@test "client/DaemonSet: fail when vault, tls are enabled but no caCert provided" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.tls.enabled=true' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.tls.caCert.secretName must be provided if global.tls.enabled=true and global.secretsBackend.vault.enabled=true." ]]
}

@test "client/DaemonSet: fail when vault, tls are enabled with a serverCert but no autoencrypt" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enabled=true' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.tls.enableAutoEncrypt must be true if global.secretsBackend.vault.enabled=true and global.tls.enabled=true" ]]
}

@test "client/DaemonSet: fail when vault is enabled with tls but autoencrypt is disabled" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.server.serverCert.secretName=test' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'global.tls.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.tls.enableAutoEncrypt must be true if global.secretsBackend.vault.enabled=true and global.tls.enabled=true" ]]
}

@test "client/DaemonSet: fail when vault is enabled with tls but no consulCARole is provided" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.server.serverCert.secretName=test' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.secretsBackend.vault.consulCARole must be provided if global.secretsBackend.vault.enabled=true and global.tls.enabled=true" ]]
}

@test "client/DaemonSet: vault annotations not set by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/role"] | length > 0 ' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: vault annotations added when vault is enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

@test "client/DaemonSet: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/client-daemonset.yaml  \
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

@test "client/DaemonSet: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is see and agentAnnotations are set without vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/agent-extra-secret: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "vns" ]
}

@test "client/DaemonSet: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.vaultNamespace=vns' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/namespace: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/namespace"]' | tee /dev/stderr)"
  [ "${actual}" = "bar" ]
}

@test "client/DaemonSet: vault gossip annotations are set when gossip encryption enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.gossipEncryption.secretName=path/to/secret' \
    --set 'global.gossipEncryption.secretKey=gossip' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-gossip.txt"]' | tee /dev/stderr)
  [ "${actual}" = "path/to/secret" ]
  local actual="$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-gossip.txt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"path/to/secret\" -}}\n{{- .Data.data.gossip -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "client/DaemonSet: GOSSIP_KEY env variable is not set and command defines GOSSIP_KEY when vault is enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
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

@test "client/DaemonSet: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

@test "client/DaemonSet: vault tls annotations are set when tls is enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'server.serverCert.secretName=pki_int/issue/test' \
    --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki_int/cert/ca" ]

  local actual="$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/cert/ca\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "client/DaemonSet: tls related volumes not attached and command is modified correctly when tls is enabled with vault" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=pki_int/ca/pem' \
    --set 'server.serverCert.secretName=pki_int/issue/test' \
    . | tee /dev/stderr |
      yq -r '.spec.template.spec' | tee /dev/stderr)


  local actual=$(echo $object |
    yq -r '.volumes[] | select(.name == "consul-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object |
    yq -r '.volumes[] | select(.name == "consul-ca-key") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object |
    yq -r '.containers[0].volumeMounts[] | select(.name == "consul-client-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object |
    yq -r '.containers[0].volumeMounts[] | select(.name == "consul-ca-key")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object |
      yq -r '.containers[0].command | any(contains("ca_file = \"/vault/secrets/serverca.crt\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: vault enterprise license annotations are correct when enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.enterpriseLicense.secretName=path/to/secret' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-enterpriselicense.txt"]' | tee /dev/stderr)
  [ "${actual}" = "path/to/secret" ]

  local actual="$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-enterpriselicense.txt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"path/to/secret\" -}}\n{{- .Data.data.enterpriselicense -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]
}

@test "client/DaemonSet: vault enterprise license annotations are not set when ent license is set and ACLs are enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.manageSystemACLsRole=aclsrole' \
    --set 'global.enterpriseLicense.secretName=path/to/secret' \
    --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
    --set 'global.acls.manageSystemACLs=true' \
    --set 'global.acls.bootstrapToken.secretName=boot' \
    --set 'global.acls.bootstrapToken.secretKey=token' \
    . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-enterpriselicense.txt"]' | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual="$(echo $object |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-enterpriselicense.txt"]' | tee /dev/stderr)"
  [ "${actual}" = "null" ]
}

@test "client/DaemonSet: vault CONSUL_LICENSE_PATH is set to /vault/secrets/enterpriselicense.txt" {
  cd `chart_dir`
  local env=$(helm template \
    -s templates/client-daemonset.yaml  \
    --set 'client.enabled=true' \
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

@test "client/DaemonSet: vault does not add volume for license secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.enterpriseLicense.secretName=a/b/c/d' \
      --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/DaemonSet: vault does not add volume mount for license secret" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.enterpriseLicense.secretName=a/b/c/d' \
      --set 'global.enterpriseLicense.secretKey=enterpriselicense' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-license")' | tee /dev/stderr)
      [ "${actual}" = "" ]
}

@test "client/DaemonSet: vault adds consul envvars CONSUL_CACERT on acl-init init container when ACLs are enabled and tls is enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.secretsBackend.vault.manageSystemACLsRole=true' \
      --set 'global.acls.replicationToken.secretName=replication' \
      --set 'global.acls.replicationToken.secretKey=key' \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.initContainers[0].env[]' | tee /dev/stderr)

  local actual=$(echo $env | jq -r '. | select(.name == "CONSUL_CACERT_FILE") | .value' | tee /dev/stderr)
    [ "${actual}" = "/vault/secrets/serverca.crt" ]
}

@test "client/DaemonSet: Vault does not add consul ca cert volumeMount to acl-init init container when ACLs are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
      . | yq '.spec.template.spec.initContainers[0].volumeMounts[] | select(.name=="consul-ca-cert")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

#--------------------------------------------------------------------
# Vault agent annotations

@test "client/DaemonSet: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."consul.hashicorp.com/config-checksum") |
      del(."vault.hashicorp.com/agent-inject") |
      del(."vault.hashicorp.com/role") |
      del(."vault.hashicorp.com/agent-init-first")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "client/DaemonSet: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# global.imageK8s

@test "client/DaemonSet: errors on global.imageK8s" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.imageK8s=something' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.imageK8s is not a valid key, use global.imageK8S (note the capital 'S')" ]]
}

#--------------------------------------------------------------------
# global.cloud
@test "client/DaemonSet: fails when global.cloud.enabled is true and global.cloud.clientId.secretName is not set but global.cloud.clientSecret.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientSecret.secretName=client-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=client-resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=client-resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "client/DaemonSet: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "client/DaemonSet: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set." ]]
}

@test "client/DaemonSet: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "client/DaemonSet: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "client/DaemonSet: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "client/DaemonSet: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/api-gateway-controller-deployment.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "client/DaemonSet: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "client/DaemonSet: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "client/DaemonSet: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.datacenter=dc-foo' \
      --set 'global.domain=bar' \
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

@test "client/DaemonSet: sets TLS server name if global.cloud.enabled is set" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq '[.env[9].name] | any(contains("CONSUL_TLS_SERVER_NAME"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[9].value] | any(contains("server.dc1.consul"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
