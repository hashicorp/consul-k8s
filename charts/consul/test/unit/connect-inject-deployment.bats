#!/usr/bin/env bats

load _helpers

@test "connectInject/Deployment: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-deployment.yaml  \
      .
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
      --set 'global.enabled=false' \
      .
}

@test "connectInject/Deployment: fails if global.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if global.enabled=true and client.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=true' \
      --set 'client.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if global.enabled=false and client.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if client.grpc=false" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'client.grpc=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "client.grpc must be true for connect injection" ]]
}

@test "connectInject/Deployment: command defaults" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("consul-k8s-control-plane inject-connect"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# connectInject.centralConfig [DEPRECATED]

@test "connectInject/Deployment: fails if connectInject.centralConfig.enabled is set to false" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=false' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.centralConfig.enabled cannot be set to false; to disable, set enable_central_service_config to false in server.extraConfig and client.extraConfig" ]]
}

@test "connectInject/Deployment: fails if connectInject.centralConfig.defaultProtocol is set" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.defaultProtocol=http' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.centralConfig.defaultProtocol is no longer supported; instead you must migrate to CRDs (see www.consul.io/docs/k8s/crds/upgrade-to-crds)" ]]
}

@test "connectInject/Deployment: fails if connectInject.centralConfig.proxyDefaults is used" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.proxyDefaults="{\"key\":\"value\"}"' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.centralConfig.proxyDefaults is no longer supported; instead you must migrate to CRDs (see www.consul.io/docs/k8s/crds/upgrade-to-crds)" ]]
}

@test "connectInject/Deployment: does not fail if connectInject.centralConfig.enabled is set to true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: does not fail if connectInject.centralConfig.proxyDefaults is set to {}" {
  cd `chart_dir`

  # We have to actually create a values file for this test because the
  # --set and --set-string flags were passing {} as a YAML object rather
  # than a string.
  # Previously this was the default in the values.yaml so this test is testing
  # that if someone had copied this into their values.yaml then nothing would
  # break. We no longer use this value, but that's okay because the default
  # empty object had no effect.
  temp_file=$(mktemp)
  cat <<EOF > "$temp_file"
connectInject:
  enabled: true
  centralConfig:
    proxyDefaults: |
      {}
EOF

  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      -f "$temp_file" \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
  rm -f "$temp_file"
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

#--------------------------------------------------------------------
# consul and envoy images

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

@test "connectInject/Deployment: envoy-image can be set via global" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.imageEnvoy=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-envoy-image=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: setting connectInject.imageEnvoy fails" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.imageEnvoy=new/image' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.imageEnvoy must be specified in global" ]]
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

@test "connectInject/Deployment: -enable-consul-dns unset by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-enable-consul-dns=true")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: -enable-consul-dns is true if dns.enabled=true and dns.enableRedirection=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'dns.enableRedirection=true' \
      . | tee /dev/stderr |
      yq -c -r '.spec.template.spec.containers[0].command | join(" ") | contains("-enable-consul-dns=true")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
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

@test "connectInject/Deployment: Adds tls-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "connectInject/Deployment: Adds tls-ca-cert volumeMounts when global.tls.enabled is true" {
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

#--------------------------------------------------------------------
# global.tls.enableAutoEncrypt

@test "connectInject/Deployment: consul-auto-encrypt-ca-cert volume is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul-auto-encrypt-ca-cert volumeMount is added when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-auto-encrypt-ca-cert") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: get-auto-encrypt-client-ca init container is created when TLS with auto-encrypt is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: adds both init containers when TLS with auto-encrypt and ACLs + namespaces are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers | length == 2' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul-ca-cert volume is not added if externalServers.enabled=true and externalServers.useSystemRoots=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.hosts[0]=foo.com' \
      --set 'externalServers.useSystemRoots=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" = "" ]
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

@test "connectInject/Deployment: fails if namespaces are disabled and mirroringK8S is true" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.enableConsulNamespaces=false' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.enableConsulNamespaces must be true if mirroringK8S=true" ]]
}

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
  [ "${actual}" = "false" ]

  local actual=$(echo $object |
    yq 'any(contains("k8s-namespace-mirroring-prefix"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: mirroring options set with .connectInject.consulNamespaces.mirroringK8S=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'connectInject.consulNamespaces.mirroringK8S=true' \
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
      yq '[.spec.template.spec.containers[0].env[].name] | any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
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
    yq 'any(contains("CONSUL_HTTP_TOKEN"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
    yq 'map(select(test("CONSUL_HTTP_TOKEN"))) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "connectInject/Deployment: consul-logout preStop hook is added when ACLs are enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].lifecycle.preStop.exec.command[2]] | any(contains("consul-k8s-control-plane consul-logout -consul-api-timeout=5s"))' | tee /dev/stderr)

  [ "${object}" = "true" ]
}

@test "connectInject/Deployment: CONSUL_HTTP_TOKEN_FILE is not set when acls are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[0].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: CONSUL_HTTP_TOKEN_FILE is set when acls are enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '[.spec.template.spec.containers[0].env[1].name] | any(contains("CONSUL_HTTP_TOKEN_FILE"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls disabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "connect-injector-acl-init" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].value] | any(contains("http://$(HOST_IP):8500"))' | tee /dev/stderr)
      echo $actual
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "connect-injector-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("CONSUL_CACERT"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].value] | any(contains("https://$(HOST_IP):8501"))' | tee /dev/stderr)
      echo $actual
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[1] | any(contains("consul-ca-cert"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command with Partitions enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.adminPartitions.enabled=true' \
      --set 'global.adminPartitions.name=default' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "connect-injector-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-acl-auth-method=release-name-consul-k8s-component-auth-method"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-partition=default"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("CONSUL_CACERT"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].value] | any(contains("https://$(HOST_IP):8501"))' | tee /dev/stderr)
      echo $actual
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[1] | any(contains("consul-ca-cert"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled and autoencrypt enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "connect-injector-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[1].name] | any(contains("CONSUL_CACERT"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].name] | any(contains("CONSUL_HTTP_ADDR"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '[.env[2].value] | any(contains("https://$(HOST_IP):8501"))' | tee /dev/stderr)
      echo $actual
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq '.volumeMounts[1] | any(contains("consul-auto-encrypt-ca-cert"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-consul-api-timeout=5s"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: auto-encrypt init container is created and is the first init-container when global.acls.manageSystemACLs=true and has correct command and environment with tls enabled and autoencrypt enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[0]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "get-auto-encrypt-client-ca" ]
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

@test "connectInject/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command when in non-primary datacenter with Consul Namespaces disabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.datacenter=dc2' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "connect-injector-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-acl-auth-method=release-name-consul-k8s-component-auth-method"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: init container is created when global.acls.manageSystemACLs=true and has correct command when in non-primary datacenter with Consul Namespaces enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.datacenter=dc2' \
      --set 'global.enableConsulNamespaces=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "connect-injector-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s-control-plane acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-acl-auth-method=release-name-consul-k8s-component-auth-method-dc2"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo $object |
      yq -r '.command | any(contains("-primary-datacenter=dc1"))' | tee /dev/stderr)
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
  [ "${actual}" = '{"limits":{"cpu":"50m","memory":"50Mi"},"requests":{"cpu":"50m","memory":"50Mi"}}' ]
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
  [ "${actual}" = '{"limits":{"cpu":"200m","memory":"200Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}' ]
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

  local actual=$(echo "$cmd" |
    yq 'any(contains("-init-container-cpu-limit=50m"))' | tee /dev/stderr)
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
# consul sidecar resources

@test "connectInject/Deployment: default consul sidecar container resources" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-request=25Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-request=20m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-limit=50Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-limit=20m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul sidecar container resources can be set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.consulSidecarContainer.resources.requests.memory=100Mi' \
      --set 'global.consulSidecarContainer.resources.requests.cpu=100m' \
      --set 'global.consulSidecarContainer.resources.limits.memory=200Mi' \
      --set 'global.consulSidecarContainer.resources.limits.cpu=200m' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-request=100Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-request=100m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-limit=200Mi"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-limit=200m"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul sidecar container resources can be set explicitly to 0" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.consulSidecarContainer.resources.requests.memory=0' \
      --set 'global.consulSidecarContainer.resources.requests.cpu=0' \
      --set 'global.consulSidecarContainer.resources.limits.memory=0' \
      --set 'global.consulSidecarContainer.resources.limits.cpu=0' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-request=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-request=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-limit=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-limit=0"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul sidecar container resources can be individually set to null" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.consulSidecarContainer.resources.requests.memory=null' \
      --set 'global.consulSidecarContainer.resources.requests.cpu=null' \
      --set 'global.consulSidecarContainer.resources.limits.memory=null' \
      --set 'global.consulSidecarContainer.resources.limits.cpu=null' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: consul sidecar container resources can be set to null" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.consulSidecarContainer.resources=null' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-memory-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-consul-sidecar-cpu-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: fails if global.lifecycleSidecarContainer is set" {
  cd `chart_dir`
  run helm template \
      -s templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'global.lifecycleSidecarContainer.resources.requests.memory=100Mi' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "global.lifecycleSidecarContainer has been renamed to global.consulSidecarContainer. Please set values using global.consulSidecarContainer." ]]
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
    yq 'any(contains("-default-sidecar-proxy-memory-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-cpu-request"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("-default-sidecar-proxy-memory-limit"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

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
# replicas

@test "connectInject/Deployment: replicas defaults to 2" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.replicas' | tee /dev/stderr)

  [ "${actual}" = "2" ]
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
# get-auto-encrypt-client-ca

@test "connectInject/Deployment: get-auto-encrypt-client-ca uses server's stateful set address by default and passes ca cert" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "get-auto-encrypt-client-ca").command | join(" ")' | tee /dev/stderr)

  # check server address
  actual=$(echo $command | jq ' . | contains("-server-addr=release-name-consul-server")')
  [ "${actual}" = "true" ]

  # check server port
  actual=$(echo $command | jq ' . | contains("-server-port=8501")')
  [ "${actual}" = "true" ]

  # check server's CA cert
  actual=$(echo $command | jq ' . | contains("-ca-file=/consul/tls/ca/tls.crt")')
  [ "${actual}" = "true" ]

  # check consul-api-timeout
  actual=$(echo $command | jq ' . | contains("-consul-api-timeout=5s")')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# enable-webhook-ca-update

@test "controller/Deployment: enable-webhook-ca-update flag is not set on command by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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
      --set 'global.tls.enableAutoEncrypt=true' \
      --set 'global.tls.caCert.secretName=foo' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
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
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
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
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
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
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
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
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.tls.caCert.secretName=foo' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=foo' \
    --set 'global.secretsBackend.vault.consulServerRole=test' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.secretsBackend.vault.ca.secretName=ca' \
    --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
    . | tee /dev/stderr |
      yq -r '.spec.template' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/agent-extra-secret"')
  [ "${actual}" = "ca" ]
  local actual=$(echo $object | yq -r '.metadata.annotations."vault.hashicorp.com/ca-cert"')
  [ "${actual}" = "/vault/custom/tls.crt" ]
}

@test "connectInject/Deployment: vault tls annotations are set when tls is enabled" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.enabled=true' \
      --set 'connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.tls.enableAutoEncrypt=true' \
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
  local expected=$'{{- with secret \"pki/issue/connect-webhook-cert-dc1\" \"common_name=connect-injector.dc1.consul\"\n\"alt_names=localhost,release-name-consul-connect-injector,*.release-name-consul-connect-injector,*.release-name-consul-connect-injector.default,release-name-consul-connect-injector.default,*.release-name-consul-connect-injector.default.svc,release-name-consul-connect-injector.default.svc,*.connect-injector.dc1.consul\" \"ip_sans=127.0.0.1\" -}}\n{{- .Data.certificate -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-secret-tls.key"]' | tee /dev/stderr)"
  [ "${actual}" = "pki/issue/connect-webhook-cert-dc1" ]

  local actual="$(echo $cmd |
      yq -r '.annotations["vault.hashicorp.com/agent-inject-template-tls.key"]' | tee /dev/stderr)"
  local expected=$'{{- with secret \"pki/issue/connect-webhook-cert-dc1\" \"common_name=connect-injector.dc1.consul\"\n\"alt_names=localhost,release-name-consul-connect-injector,*.release-name-consul-connect-injector,*.release-name-consul-connect-injector.default,release-name-consul-connect-injector.default,*.release-name-consul-connect-injector.default.svc,release-name-consul-connect-injector.default.svc,*.connect-injector.dc1.consul\" \"ip_sans=127.0.0.1\" -}}\n{{- .Data.private_key -}}\n{{- end -}}'
  [ "${actual}" = "${expected}" ]

}

@test "connectInject/Deployment: vault does not add tls-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "connectInject/Deployment: vault does not add tls-ca-cert volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "connectInject/Deployment: vault tls-cert-dir flag is set to /vault/secrets" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
            . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-cert-dir=/vault/secrets"))' | tee /dev/stderr)

  [ "${actual}" = "true" ]
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
      yq -r '.spec.template.metadata.annotations | del(."consul.hashicorp.com/connect-inject") | del(."vault.hashicorp.com/agent-inject") | del(."vault.hashicorp.com/role")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "connectInject/Deployment: vault agent annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
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
