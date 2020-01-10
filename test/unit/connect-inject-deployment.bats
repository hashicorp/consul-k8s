#!/usr/bin/env bats

load _helpers

@test "connectInject/Deployment: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: enable with global.enabled false, client.enabled true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: disable with connectInject.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: fails if global.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if global.enabled=true and client.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=true' \
      --set 'client.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if global.enabled=false and client.enabled=false" {
  cd `chart_dir`
  run helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "clients must be enabled for connect injection" ]]
}

@test "connectInject/Deployment: fails if client.grpc=false" {
  cd `chart_dir`
  run helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'client.grpc=false' \
      --set 'connectInject.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "client.grpc must be true for connect injection" ]]
}

#--------------------------------------------------------------------
# consul and envoy images

@test "connectInject/Deployment: container image is global default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.imageK8S=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "\"foo\"" ]
}

@test "connectInject/Deployment: container image overrides" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
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
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.image=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-image=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: consul-image can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'global.image=foo' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.imageConsul=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-consul-image=\"bar\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: envoy-image is not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-envoy-image"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: envoy-image can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.imageEnvoy=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-envoy-image=\"foo\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# cert secrets

@test "connectInject/Deployment: no secretName: no tls-{cert,key}-file set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-cert-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-key-file"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-auto"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: with secretName: tls-{cert,key}-file set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.certs.secretName=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-cert-file"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.certs.secretName=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-key-file"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.certs.secretName=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-tls-auto"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}


#--------------------------------------------------------------------
# service account name

@test "connectInject/Deployment: with secretName: no serviceAccountName set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.certs.secretName=foo' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.serviceAccountName | has("serviceAccountName")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: no secretName: serviceAccountName set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.serviceAccountName | contains("connect-injector-webhook-svc-account")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# nodeSelector

@test "connectInject/Deployment: nodeSelector is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "connectInject/Deployment: nodeSelector is not set by default with sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "connectInject/Deployment: specified nodeSelector" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.nodeSelector=testing' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "testing" ]
}

#--------------------------------------------------------------------
# centralConfig

@test "connectInject/Deployment: centralConfig is enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-central-config"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: centralConfig can be disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-enable-central-config"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: defaultProtocol is disabled by default with centralConfig enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-default-protocol"))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: defaultProtocol can be enabled with centralConfig enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      --set 'connectInject.centralConfig.defaultProtocol=grpc' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-default-protocol=\"grpc\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# authMethod

@test "connectInject/Deployment: -acl-auth-method is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method="))' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/Deployment: -acl-auth-method is set when global.bootstrapACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"release-name-consul-k8s-auth-method\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: -acl-auth-method is set to connectInject.overrideAuthMethodName" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.overrideAuthMethodName=override' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"override\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Deployment: -acl-auth-method is overridden by connectInject.overrideAuthMethodName if global.bootstrapACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      --set 'connectInject.overrideAuthMethodName=override' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-acl-auth-method=\"override\""))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.tls.enabled

@test "connectInject/Deployment: Adds tls-ca-cert volume when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "connectInject/Deployment: Adds both tls-ca-cert and certs volumes when global.tls.enabled is true and connectInject.certs.secretName is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "connectInject/Deployment: Adds tls-ca-cert volumeMounts when global.tls.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "connectInject/Deployment: Adds both tls-ca-cert and certs volumeMounts when global.tls.enabled is true and connectInject.certs.secretName is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}
