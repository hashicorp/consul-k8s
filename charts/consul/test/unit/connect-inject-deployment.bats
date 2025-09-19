#!/usr/bin/env bats

load _helpers

@test "connectInject/Deployment: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: enable with global.enabled false, client.enabled true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: disable with connectInject.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=false' \
      .
}

@test "connectInject/Deployment: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=-' \
      --set 'global.enabled=false' \
      .
}

@test "connectInject/Deployment: consul env defaults" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_ADDRESSES").value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-server.default.svc" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_GRPC_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8502" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_HTTP_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8500" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_DATACENTER").value' | tee /dev/stderr)
  [ "${actual}" = "dc1" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_API_TIMEOUT").value' | tee /dev/stderr)
  [ "${actual}" = "5s" ]
}

#--------------------------------------------------------------------
# metrics

@test "connectInject/Deployment: default connect-inject metrics flags" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-metrics=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-metrics-merging=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-merged-metrics-port=20100"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-prometheus-scrape-port=20200"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-prometheus-scrape-path=\"/metrics\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: adds flag default-enable-metrics=true when global.metrics.enabled=true" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-metrics=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: adds flag default-enable-metrics=true when metrics.defaultEnabled=true and global.metrics.enabled=false" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.metrics.defaultEnabled=true' \
      --set 'global.metrics.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-metrics=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: adds flag default-enable-metrics=false when metrics.defaultEnabled=false and global.metrics.enabled=true" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=true' \
      --set 'connectInject.metrics.defaultEnabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-metrics=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: adds flag default-enable-metrics=false when global.metrics.enabled=false" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-metrics=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: metrics.defaultEnableMerging can be configured" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.metrics.defaultEnableMerging=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-metrics-merging=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: metrics.defaultMergedMetricsPort can be configured" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.metrics.defaultMergedMetricsPort=12345' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-merged-metrics-port=12345"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: metrics.defaultPrometheusScrapePort can be configured" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.metrics.defaultPrometheusScrapePort=12345' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-prometheus-scrape-port=12345"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: metrics.defaultPrometheusScrapePath can be configured" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.metrics.defaultPrometheusScrapePath=/some-path' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-prometheus-scrape-path=\"/some-path\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: metrics.enableTelemetryCollector can be configured" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.metrics.enableTelemetryCollector=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-enable-telemetry-collector=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
#--------------------------------------------------------------------
# consul and consul-dataplane images

@test "connectInject/Deployment: container image is global default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.imageK8S=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "\"foo\"" ]
}

@test "connectInject/Deployment: container image overrides" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.imageK8S=foo' \
      --set 'connectInject.image=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "\"bar\"" ]
}

@test "connectInject/Deployment: consul-image defaults to global" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.image=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-image=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul-image can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.image=foo' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.imageConsul=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-image=\"bar\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul-dataplane-image can be set via global" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.imageConsulDataplane=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-dataplane-image=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# extra envoy args

@test "connectInject/Deployment: extra envoy args can be set via connectInject" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.envoyExtraArgs=--foo bar --boo baz' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-envoy-extra-args=\"--foo bar --boo baz\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: extra envoy args are not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-envoy-extra-args"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}


#--------------------------------------------------------------------
# affinity

@test "connectInject/Deployment: affinity not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.affinity == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: affinity can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.affinity=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .affinity == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "connectInject/Deployment: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "connectInject/Deployment: nodeSelector is not set by default with sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "connectInject/Deployment: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# tolerations

@test "connectInject/Deployment: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.tolerations == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.tolerations=foobar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .tolerations == "foobar"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# authMethod

@test "connectInject/Deployment: -acl-auth-method is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method="))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: -acl-auth-method is set when global.acls.manageSystemACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"release-name-consul-k8s-auth-method\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: -acl-auth-method is set to connectInject.overrideAuthMethodName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.overrideAuthMethodName=override' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"override\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: -acl-auth-method is overridden by connectInject.overrideAuthMethodName if global.acls.manageSystemACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'connectInject.overrideAuthMethodName=override' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"override\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# DNS

@test "connectInject/Deployment: -enable-consul-dns is set by default due to inheriting from connectInject.transparentProxy.defaultEnabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-enable-consul-dns=true")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: -enable-consul-dns is true if dns.enabled=true and dns.enableRedirection=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'dns.enableRedirection=true' \
      --set 'dns.enabled=true' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-enable-consul-dns=true")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: -enable-consul-dns is not set when connectInject.transparentProxy.defaultEnabled is false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-enable-consul-dns=true")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: -enable-consul-dns is not set if dns.enabled is false or ens.enableRedirection is false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'dns.enabled=false' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-enable-consul-dns=true")' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'dns.enableRedirection=false' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-enable-consul-dns=true")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: -resource-prefix always set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-resource-prefix=release-name-consul")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "connectInject/Deployment: Adds consul-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "connectInject/Deployment: Adds consul-ca-cert volumeMount when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "connectInject/Deployment: can overwrite CA secret with the provided one" {
  cd `chart_dir`
  local ca_cert_volume=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: consul env vars when global.tls.enabled is true" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_HTTP_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8501" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_USE_TLS").value' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_CACERT_FILE").value' | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]
}

#--------------------------------------------------------------------
# k8sAllowNamespaces & k8sDenyNamespaces

@test "connectInject/Deployment: default is allow '*', deny nothing" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'map(select(test("allow-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'any(contains("allow-k8s-namespace=\"*\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'map(select(test("deny-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

@test "connectInject/Deployment: can set allow and deny" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.k8sAllowNamespaces[0]=allowNamespace' \
      --set 'connectInject.k8sDenyNamespaces[0]=denyNamespace' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'map(select(test("allow-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'map(select(test("deny-k8s-namespace"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  local actual=$(echo $object |
    yq 'any(contains("allow-k8s-namespace=\"allowNamespace\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("deny-k8s-namespace=\"denyNamespace\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# partitions

@test "connectInject/Deployment: partitions options disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("enable-partitions"))' | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: partitions set with .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("enable-partitions"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul env var default set with .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_PARTITION").value' | tee /dev/stderr)
  [ "${actual}" = "default" ]
}

@test "connectInject/Deployment: consul env var set with .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=foo' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_PARTITION").value' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

@test "connectInject/Deployment: fails if namespaces are disabled and .global.adminPartitions.enabled=true" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.enableConsulNamespaces must be true if global.adminPartitions.enabled=true" ]]
}

#--------------------------------------------------------------------
# namespaces

@test "connectInject/Deployment: namespace options disabled by default" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: namespace options set with .global.enableConsulNamespaces=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: mirroring options omitted with .connectInject.consulNamespaces.mirroringK8S=false" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: prefix can be set with .connectInject.consulNamespaces.mirroringK8SPrefix" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      --set 'connectInject.consulNamespaces.mirroringK8SPrefix=k8s-' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("enable-namespaces=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("consul-destination-namespace=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("enable-k8s-namespace-mirroring=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix=k8s-"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# acl tokens

@test "connectInject/Deployment: aclInjectToken disabled when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.aclInjectToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: aclInjectToken disabled when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.aclInjectToken.secretName=foo' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: aclInjectToken enabled when secretName and secretKey is provided" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.aclInjectToken.secretName=foo' \
      --set 'connectInject.aclInjectToken.secretKey=bar' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[].name]' | tee /dev/stderr)

  local actual=$(echo $object |
    yq 'any(contains("CONSUL_ACL_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'map(select(test("CONSUL_ACL_TOKEN"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "connectInject/Deployment: ACL auth method env vars are set when acls are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_AUTH_METHOD").value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-k8s-component-auth-method" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_DATACENTER").value' | tee /dev/stderr)
  [ "${actual}" = "dc1" ]
  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_META").value' | tee /dev/stderr)
  [ "${actual}" = 'component=connect-injector,pod=$(NAMESPACE)/$(POD_NAME)' ]
}

@test "connectInject/Deployment: sets global auth method and primary datacenter when federation and acls" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'global.datacenter=dc2' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_AUTH_METHOD").value' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-k8s-component-auth-method-dc2" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_DATACENTER").value' | tee /dev/stderr)
  [ "${actual}" = "dc1" ]
}

@test "connectInject/Deployment: sets default login partition and acls and partitions are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_PARTITION").value' | tee /dev/stderr)
  [ "${actual}" = "default" ]
}

@test "connectInject/Deployment: sets non-default login partition and acls and partitions are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=foo' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_LOGIN_PARTITION").value' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

@test "connectInject/Deployment: cross namespace policy is not added when global.acls.manageSystemACLs=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-cross-namespace-acl-policy"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: cross namespace policy is added when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-cross-namespace-acl-policy"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# resources

@test "connectInject/Deployment: default resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"requests":{"cpu":"50m","memory":"200Mi"},"limits":{"memory":"200Mi"}}' ]
}

@test "connectInject/Deployment: can set resources" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.resources.requests.memory=100Mi' \
      --set 'connectInject.resources.requests.cpu=100m' \
      --set 'connectInject.resources.limits.memory=200Mi' \
      --set 'connectInject.resources.limits.cpu=200m' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"requests":{"cpu":"100m","memory":"100Mi"},"limits":{"cpu":"200m","memory":"200Mi"}}' ]
}

#--------------------------------------------------------------------
# init container resources

@test "connectInject/Deployment: default init container resources" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-request=25Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-request=50m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-limit=150Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

}

@test "connectInject/Deployment: can set init container resources" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.initContainer.resources.requests.memory=100Mi' \
      --set 'connectInject.initContainer.resources.requests.cpu=100m' \
      --set 'connectInject.initContainer.resources.limits.memory=200Mi' \
      --set 'connectInject.initContainer.resources.limits.cpu=200m' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-request=100Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-request=100m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-limit=200Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-limit=200m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: init container resources can be set explicitly to 0" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.initContainer.resources.requests.memory=0' \
      --set 'connectInject.initContainer.resources.requests.cpu=0' \
      --set 'connectInject.initContainer.resources.limits.memory=0' \
      --set 'connectInject.initContainer.resources.limits.cpu=0' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-request=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-request=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-limit=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-limit=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: init container resources can be individually set to null" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.initContainer.resources.requests.memory=null' \
      --set 'connectInject.initContainer.resources.requests.cpu=null' \
      --set 'connectInject.initContainer.resources.limits.memory=null' \
      --set 'connectInject.initContainer.resources.limits.cpu=null' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: init container resources can be set to null" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.initContainer.resources=null' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-memory-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# sidecarProxy.resources

@test "connectInject/Deployment: by default there are no resource settings" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-cpu-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: can set resource settings" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.resources.requests.memory=10Mi' \
      --set 'connectInject.sidecarProxy.resources.requests.cpu=100m' \
      --set 'connectInject.sidecarProxy.resources.limits.memory=20Mi' \
      --set 'connectInject.sidecarProxy.resources.limits.cpu=200m' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-memory-request=10Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-cpu-request=100m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-memory-limit=20Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-cpu-limit=200m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: can set resource settings explicitly to 0" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.resources.requests.memory=0' \
      --set 'connectInject.sidecarProxy.resources.requests.cpu=0' \
      --set 'connectInject.sidecarProxy.resources.limits.memory=0' \
      --set 'connectInject.sidecarProxy.resources.limits.cpu=0' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-memory-request=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-cpu-request=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-memory-limit=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-cpu-limit=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# sidecarProxy.concurrency

@test "connectInject/Deployment: by default envoy concurrency is set to 2" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-envoy-proxy-concurrency=2"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: envoy concurrency can bet set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.concurrency=4' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-envoy-proxy-concurrency=4"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# sidecarProxy.lifecycle

@test "connectInject/Deployment: by default sidecar proxy lifecycle management is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-sidecar-proxy-lifecycle"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: sidecar proxy lifecycle management can be disabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.lifecycle.defaultEnabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-sidecar-proxy-lifecycle=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: by default sidecar proxy lifecycle management shutdown listener draining is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-sidecar-proxy-lifecycle-shutdown-drain-listeners"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: sidecar proxy lifecycle management shutdown listener draining can be disabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.lifecycle.defaultEnableShutdownDrainListeners=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-enable-sidecar-proxy-lifecycle-shutdown-drain-listeners=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: by default sidecar proxy lifecycle management shutdown grace period is set to 30 seconds" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-shutdown-grace-period-seconds=30"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: sidecar proxy lifecycle management shutdown grace period can be set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.lifecycle.defaultShutdownGracePeriodSeconds=23' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-shutdown-grace-period-seconds=23"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: by default sidecar proxy lifecycle management startup grace period is set to 0 seconds" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-startup-grace-period-seconds=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: sidecar proxy lifecycle management startup grace period can be set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.lifecycle.defaultStartupGracePeriodSeconds=13' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-startup-grace-period-seconds=13"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: by default sidecar proxy lifecycle management port is set to 20600" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-graceful-port=20600"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: sidecar proxy lifecycle management port can be set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.lifecycle.defaultGracefulPort=20307' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-graceful-port=20307"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: by default sidecar proxy lifecycle management graceful shutdown path is set to /graceful_shutdown" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-graceful-shutdown-path=\"/graceful_shutdown\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: sidecar proxy lifecycle management graceful shutdown path can be set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.lifecycle.defaultGracefulShutdownPath=/exit' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-graceful-shutdown-path=\"/exit\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: by default sidecar proxy lifecycle management graceful startup path is set to /graceful_startup" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-graceful-startup-path=\"/graceful_startup\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: sidecar proxy lifecycle management graceful startup path can be set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.sidecarProxy.lifecycle.defaultGracefulStartupPath=/start' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-lifecycle-graceful-startup-path=\"/start\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# priorityClassName

@test "connectInject/Deployment: no priorityClassName by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "connectInject/Deployment: can set a priorityClassName" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.priorityClassName=name' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.priorityClassName' | tee /dev/stderr)

  [ "${actual}" = "name" ]
}

#--------------------------------------------------------------------
# extraLabels

@test "connectInject/Deployment: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "connectInject/Deployment: can set extra labels" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.extraLabels.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)

  [ "${actual}" = "bar" ]
}

@test "connectInject/Deployment: can set extra global labels" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "connectInject/Deployment: can set multiple extra global labels" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "connectInject/Deployment: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# logLevel

@test "connectInject/Deployment: logLevel info by default from global" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=info"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: logLevel can be overridden" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.logLevel=debug' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-log-level=debug"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# transparent proxy

@test "connectInject/Deployment: transparent proxy is enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-transparent-proxy=true"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: transparent proxy can be disabled by setting connectInject.transparentProxy.defaultEnabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-transparent-proxy=false"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: overwrite probes is enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-transparent-proxy-default-overwrite-probes=true"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: overwrite probes can be disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.transparentProxy.defaultOverwriteProbes=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-transparent-proxy-default-overwrite-probes=false"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# cni

@test "connectInject/Deployment: cni is disabled by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-enable-cni=false"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: cni can be enabled by setting connectInject.cni.enabled=true" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-enable-cni=true"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# peering

@test "connectInject/Deployment: peering is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-peering=true"))' | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: -enable-peering=true is set when global.peering.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.peering.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-peering=true"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: fails if peering is enabled but connect inject is not" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=false' \
      --set 'global.peering.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "setting global.peering.enabled to true requires connectInject.enabled to be true" ]]
}

@test "connectInject/Deployment: fails if peering is enabled but tls is not" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.peering.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "setting global.peering.enabled to true requires global.tls.enabled to be true" ]]
}

@test "connectInject/Deployment: fails if peering is enabled but mesh gateways are not" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.peering.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "setting global.peering.enabled to true requires meshGateway.enabled to be true" ]]
}

#--------------------------------------------------------------------
# openshift

@test "connectInject/Deployment: openshift is is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-openshift"))' | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: -enable-openshift is set when global.openshift.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-openshift"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeMeta

@test "connectInject/Deployment: nodeMeta is not set by default" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-node-meta"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: can set nodeMeta explicitly" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.consulNode.meta.foo=bar' \
      --set 'connectInject.consulNode.meta.test=value' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-node-meta=foo=bar"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-node-meta=test=value"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# replicas

@test "connectInject/Deployment: replicas defaults to 1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "1" ]
}

@test "connectInject/Deployment: replicas can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.replicas=3' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "3" ]
}

#--------------------------------------------------------------------
# Vault

@test "connectInject/Deployment: CONSUL_CACERT env variable is set points to vault secrets when TLS and vault are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env[] | select(.name == "CONSUL_CACERT_FILE").value' | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/serverca.crt" ]
}

@test "connectInject/Deployment: consul-ca-cert volume is not added when TLS and vault are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "connectInject/Deployment: consul-ca-cert volume mount is not added when TLS and vault are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers.volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "connectInject/Deployment: vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is see and agentAnnotations are set without vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: correct vault namespace annotations is set when global.secretsBackend.vault.vaultNamespace is set and agentAnnotations are also set with vaultNamespace annotation" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
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

#--------------------------------------------------------------------
# enable-webhook-ca-update

@test "connectInject/Deployment: enable-webhook-ca-update flag is not set on command by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-webhook-ca-update"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: enable-webhook-ca-update flag is not set on command when using vault" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
      --set 'global.secretsBackend.vault.connectInjectRole=test' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=foo/ca' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=foo/tls' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-webhook-ca-update"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault

@test "connectInject/Deployment: vault CA is not configured by default" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/connect-inject-deployment.yaml  \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test2' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: vault CA is not configured when secretName is set but secretKey is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/connect-inject-deployment.yaml  \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test2' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: vault CA is not configured when secretKey is set but secretName is not" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/connect-inject-deployment.yaml  \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test2' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/agent-extra-secret")')
  [ "${actual}" = "false" ]
  local actual=$(echo $object | yq -r '.metadata.annotations | has("vault.hashicorp.com/ca-cert")')
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: vault CA is configured when both secretName and secretKey are set" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/connect-inject-deployment.yaml  \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test2' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

@test "connectInject/Deployment: fails if vault is enabled and global.secretsBackend.vault.connectInjectRole is set but global.secretsBackend.vault.connectInject.tlsCert.secretName and global.secretsBackend.vault.connectInject.caCert.secretName are not" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.connectInjectRole=connectinjectcarole' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When one of the following has been set, all must be set:  global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, global.secretsBackend.vault.connectInject.caCert.secretName" ]]
}

@test "connectInject/Deployment: fails if vault is enabled and global.secretsBackend.vault.connectInject.tlsCert.secretName is set but global.secretsBackend.vault.connectInjectRole and global.secretsBackend.vault.connectInject.caCert.secretName are not" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=connectInject/Deployment: enable-webhook-ca-update flag is not set on command when using vaulttest' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=foo/tls' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When one of the following has been set, all must be set:  global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, global.secretsBackend.vault.connectInject.caCert.secretName" ]]
}

@test "connectInject/Deployment: fails if vault is enabled and global.secretsBackend.vault.connectInject.caCert.secretName is set but global.secretsBackend.vault.connectInjectRole and global.secretsBackend.vault.connectInject.tlsCert.secretName are not" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=foo/ca' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "When one of the following has been set, all must be set:  global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, global.secretsBackend.vault.connectInject.caCert.secretName" ]]
}

@test "connectInject/Deployment: vault tls annotations are set when tls is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      --set 'global.tls.enabled=true' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
      --set 'global.secretsBackend.vault.connectInjectRole=test' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=foo/ca' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/cert/ca\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki_int/cert/ca" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-ca.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"foo/ca\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-ca.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "foo/ca" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/secret-volume-path-ca.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "/vault/secrets/connect-injector/certs" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-init-first"]' | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject"]' | tee /dev/stderr)"
  [ "${actual}" = "true" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)"
  [ "${actual}" = "test" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-tls.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki/issue/connect-webhook-cert-dc1" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-tls.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki/issue/connect-webhook-cert-dc1\" \"common_name=release-name-consul-connect-injector\"\n\"alt_names=release-name-consul-connect-injector,release-name-consul-connect-injector.default,release-name-consul-connect-injector.default.svc,release-name-consul-connect-injector.default.svc.cluster.local\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/secret-volume-path-tls.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "/vault/secrets/connect-injector/certs" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-tls.key"]' | tee /dev/stderr)"
  [ "${actual}" = "pki/issue/connect-webhook-cert-dc1" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-tls.key"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki/issue/connect-webhook-cert-dc1\" \"common_name=release-name-consul-connect-injector\"\n\"alt_names=release-name-consul-connect-injector,release-name-consul-connect-injector.default,release-name-consul-connect-injector.default.svc,release-name-consul-connect-injector.default.svc.cluster.local\" -}}\n{{- .Data.private_key -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/secret-volume-path-tls.key"]' | tee /dev/stderr)"
  [ "${actual}" = "/vault/secrets/connect-injector/certs" ]
}

@test "connectInject/Deployment: vault tls-cert-dir flag is set to /vault/secrets/connect-injector/certs" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.connectInjectRole=inject-ca-role' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
                 . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-cert-dir=/vault/secrets/connect-injector/certs"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: vault ca annotations are set when tls is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.connectInjectRole=inject-ca-role' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'server.serverCert.secretName=pki_int/issue/test' \
      --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki_int/cert/ca\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"]' | tee /dev/stderr)"
  [ "${actual}" = "pki_int/cert/ca" ]
}

@test "connectInject/Deployment: vault does not add certs volume when global.secretsBackend.vault.connectInject.tlsCert.secretName is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.connectInjectRole=inject-ca-role' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "certs")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "connectInject/Deployment: vault does not add certs volumeMounts when global.secretsBackend.vault.connectInject.tlsCert.secretName is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.connectInjectRole=inject-ca-role' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "certs")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "connectInject/Deployment: vault vault.hashicorp.com/role set to global.secretsBackend.vault.consulCARole if global.secretsBackend.vault.connectInjectRole is not set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata' | tee /dev/stderr)

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/role"]' | tee /dev/stderr)"
  [ "${actual}" = "carole" ]
}

#--------------------------------------------------------------------
# Vault agent annotations

@test "connectInject/Deployment: no vault agent annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."vault.hashicorp.com/agent-inject") |
      del(."vault.hashicorp.com/role")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "connectInject/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.agentAnnotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

# consulDestinationNamespace reserved name

@test "connectInject/Deployment: fails when consulDestinationNamespace=system" {
  reservedNameTest "system"
}

@test "connectInject/Deployment: fails when consulDestinationNamespace=universal" {
  reservedNameTest "universal"
}

@test "connectInject/Deployment: fails when consulDestinationNamespace=operator" {
  reservedNameTest "operator"
}

@test "connectInject/Deployment: fails when consulDestinationNamespace=root" {
  reservedNameTest "root"
}

# reservedNameTest is a helper function that tests if certain Consul destination
# namespace names fail because the name is reserved.
reservedNameTest() {
  cd `chart_dir`
  local -r name="$1"
		run helm template \
				-s templates/connect-inject-deployment.yaml  \
				--set 'connectInject.enabled=true' \
				--set "connectInject.consulNamespaces.consulDestinationNamespace=$name" .

		[ "$status" -eq 1 ]
		[[ "$output" =~ "The name $name set for key connectInject.consulNamespaces.consulDestinationNamespace is reserved by Consul for future use" ]]
}

#--------------------------------------------------------------------
# externalServers

@test "connectInject/Deployment: fails if externalServers.hosts is not provided when externalServers.enabled is true" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
       .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.hosts must be set if externalServers.enabled is true" ]]
}

@test "connectInject/Deployment: configures the sidecar-injector env to use external servers" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      . | tee /dev/stderr |
       yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)\

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_ADDRESSES").value' | tee /dev/stderr)
  [ "${actual}" = "consul" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_HTTP_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8501" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_GRPC_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "8502" ]
}

@test "connectInject/Deployment: can provide a different ports for the sidecar-injector when external servers are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      --set 'externalServers.httpsPort=443' \
      --set 'externalServers.grpcPort=444' \
      . | tee /dev/stderr |
       yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)\

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_ADDRESSES").value' | tee /dev/stderr)
  [ "${actual}" = "consul" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_HTTP_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "443" ]

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_GRPC_PORT").value' | tee /dev/stderr)
  [ "${actual}" = "444" ]
}

@test "connectInject/Deployment: can provide a TLS server name for the sidecar-injector when external servers are enabled" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'global.tls.enabled=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      --set 'externalServers.tlsServerName=foo' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_TLS_SERVER_NAME").value' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

@test "connectInject/Deployment: does not configure CA cert for the sidecar-injector when external servers with useSystemRoots are enabled" {
  cd `chart_dir`
  local spec=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      --set 'externalServers.useSystemRoots=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$spec" | yq '.containers[0].env[] | select(.name == "CONSUL_CACERT_FILE")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo "$spec" | yq '.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo "$spec" | yq '.initContainers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo "$spec" | yq '.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "connectInject/Deployment: fails if externalServers.skipServerWatch is not provided when externalServers.enabled is true" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.skipServerWatch=true' \
       .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "externalServers.enabled must be set if externalServers.skipServerWatch is true" ]]
}

@test "connectInject/Deployment: configures the sidecar-injector env to skip server watch" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=consul' \
      --set 'externalServers.skipServerWatch=true' \
      . | tee /dev/stderr |
       yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)\

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_SKIP_SERVER_WATCH").value' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.cloud

@test "connectInject/Deployment: fails when global.cloud.enabled is true and global.cloud.clientId.secretName is not set but global.cloud.clientSecret.secretName and global.cloud.resourceId.secretName is set" {
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

@test "connectInject/Deployment: fails when global.cloud.enabled is true and global.cloud.clientSecret.secretName is not set but global.cloud.clientId.secretName and global.cloud.resourceId.secretName is set" {
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

@test "connectInject/Deployment: fails when global.cloud.enabled is true and global.cloud.resourceId.secretName is not set but global.cloud.clientId.secretName and global.cloud.clientSecret.secretName is set" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: fails when global.cloud.resourceId.secretName is set but global.cloud.resourceId.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: fails when global.cloud.authURL.secretName is set but global.cloud.authURL.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: fails when global.cloud.authURL.secretKey is set but global.cloud.authURL.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: fails when global.cloud.apiHost.secretName is set but global.cloud.apiHost.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: fails when global.cloud.apiHost.secretKey is set but global.cloud.apiHost.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: fails when global.cloud.scadaAddress.secretName is set but global.cloud.scadaAddress.secretKey is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: fails when global.cloud.scadaAddress.secretKey is set but global.cloud.scadaAddress.secretName is not set." {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
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

@test "connectInject/Deployment: sets TLS server name if global.cloud.enabled is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-server-name=server.dc1.consul"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: validates that externalServers.hosts is not set with an HCP-managed cluster's address" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=abc.aws.hashicorp.cloud' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
     . > /dev/stderr

  [ "$status" -eq 1 ]

  [[ "$output" =~ "global.cloud.enabled cannot be used in combination with an HCP-managed cluster address in externalServers.hosts. global.cloud.enabled is for linked self-managed clusters." ]]
}

@test "connectInject/Deployment: can provide a TLS server name for the sidecar-injector when global.cloud.enabled is set" {
  cd `chart_dir`
  local env=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.cloud.enabled=true' \
      --set 'global.cloud.clientId.secretName=client-id-name' \
      --set 'global.cloud.clientId.secretKey=client-id-key' \
      --set 'global.cloud.clientSecret.secretName=client-secret-id-name' \
      --set 'global.cloud.clientSecret.secretKey=client-secret-id-key' \
      --set 'global.cloud.resourceId.secretName=resource-id-name' \
      --set 'global.cloud.resourceId.secretKey=resource-id-key' \
     . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].env[]' | tee /dev/stderr)

  local actual=$(echo "$env" |
    jq -r '. | select( .name == "CONSUL_TLS_SERVER_NAME").value' | tee /dev/stderr)
  [ "${actual}" = "server.dc1.consul" ]
}
