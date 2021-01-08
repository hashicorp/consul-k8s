#!/usr/bin/env bats

load _helpers


#--------------------------------------------------------------------
# config-configmap

@test "client/DaemonSet: adds config-checksum annotation when extraConfig is blank" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  if [[ $(v2) ]]; then
    [ "${actual}" = 79e3ac58b3bbfec6ef27d39e3e0f25e7dab63b5cc76d15f4935f308c94a5ff11 ]
  else
    [ "${actual}" = db1cb14f20d2a2f9fe0b3a1f5a65446a32126faeeadf3813f9fe610ba8ee549b ]
  fi
}

@test "client/DaemonSet: adds config-checksum annotation when extraConfig is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.extraConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  if [[ $(v2) ]]; then
    [ "${actual}" = f3d2e7d13e5ef853ae79b8c528b263022e2e2f7689aa4b490393f14128a389eb ]
  else
    [ "${actual}" = 4e3576ef3ecf806b27906771411df25ff9fe5cc30ea20ea02f7890de944ecd32 ]
  fi
}

@test "client/DaemonSet: adds config-checksum annotation when client config is updated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/config-checksum"' | tee /dev/stderr)
  if [[ $(v2) ]]; then
    [ "${actual}" = ab9a9f78fc78a0c7509ec21d2f270052c556c28a1eceb26e4b40ef0068a34889 ]
  else
    [ "${actual}" = 56e95d32e8bc31f566498a05dc8e1e08b2cb049880ce589d2e9143f45308638c ]
  fi
}

#--------------------------------------------------------------------
# tolerations

@test "client/DaemonSet: tolerations not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec | .tolerations? == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
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
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "client/DaemonSet: gossip encryption disabled in client DaemonSet when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.gossipEncryption.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "client/DaemonSet: gossip encryption disabled in client DaemonSet when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.gossipEncryption.secretName=foo' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .env[] | select(.name == "GOSSIP_KEY") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "client/DaemonSet: gossip environment variable present in client DaemonSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
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
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[] | select(.name=="consul") | .command | join(" ") | contains("encrypt")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/DaemonSet: encrypt CLI option present in client DaemonSet when all config is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
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
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: CA key volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-ca-key")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: client certificate volume present when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[] | select(.name == "consul-client-cert")' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: port 8501 is not exposed when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8501)' | tee /dev/stderr)
  [ "${actual}" == "" ]
}

@test "client/DaemonSet: port 8501 is exposed when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].ports[] | select (.containerPort == 8501)' | tee /dev/stderr)
  [ "${actual}" != "" ]
}

@test "client/DaemonSet: port 8500 is still exposed when httpsOnly is not enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
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
      --set 'global.tls.enabled=false' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("http://127.0.0.1:8500")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: readiness checks are over HTTPS when TLS is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("https://127.0.0.1:8501")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: readiness checks skip TLS verification when TLS is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].readinessProbe.exec.command | join(" ") | contains("-k")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: HTTP port is disabled when global.tls.httpsOnly is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ") | contains("ports { http = -1 }")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: init container is created when global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-tls-init") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: both ACL and TLS init containers are created when global.tls.enabled=true and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local has_acl_init_container=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init") | length > 0' | tee /dev/stderr)

  [ "${has_acl_init_container}" = "true" ]

  local has_tls_init_container=$(helm template \
      -s templates/client-daemonset.yaml  \
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

@test "client/DaemonSet: doesn't set the verify_* flags when global.tls.enabled is true and global.tls.verify is false" {
  cd `chart_dir`
  local command=$(helm template \
      -s templates/client-daemonset.yaml  \
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
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | join(" ")' | tee /dev/stderr)

  # enables auto encrypt on the client
  actual=$(echo $command | jq -r '. | contains("auto_encrypt = {tls = true}")' | tee /dev/stderr)
  [ "${actual}" == "true" ]

  # sets IP SANs to contain the HOST IP of the client
  actual=$(echo $command | jq -r '. | contains("auto_encrypt = {ip_san = [\\\"$HOST_IP\\\"]}")' | tee /dev/stderr)
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
      --set 'client.extraEnvironmentVars.custom_proxy=fakeproxy' \
      --set 'client.extraEnvironmentVars.no_proxy=custom_no_proxy' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].env' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.[] | select(.name=="custom_proxy").value' | tee /dev/stderr)
  [ "${actual}" = "fakeproxy" ]

  local actual=$(echo $object |
      yq -r '.[] | select(.name=="no_proxy").value' | tee /dev/stderr)
  [ "${actual}" = "custom_no_proxy" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "client/DaemonSet: aclconfig volume is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[2].name == "aclconfig"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: aclconfig volumeMount is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].volumeMounts[2]' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.name' | tee /dev/stderr)
  [ "${actual}" = "aclconfig" ]

  local actual=$(echo $object |
      yq -r '.mountPath' | tee /dev/stderr)
  [ "${actual}" = "/consul/aclconfig" ]
}

@test "client/DaemonSet: command includes aclconfig dir when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("/consul/aclconfig"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: init container is created when global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.initContainers[] | select(.name == "client-acl-init")' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.command | any(contains("consul-k8s acl-init"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# client.exposeGossipPorts

@test "client/DaemonSet: client uses podIP when client.exposeGossipPorts=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.exposeGossipPorts=false' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers | map(select(.name=="consul")) | .[0].env | map(select(.name=="ADVERTISE_IP")) | .[0] | .valueFrom.fieldRef.fieldPath'  |
      tee /dev/stderr)
  [ "${actual}" = "status.podIP" ]
}

@test "client/DaemonSet: client uses hostIP when client.exposeGossipPorts=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.exposeGossipPorts=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers | map(select(.name=="consul")) | .[0].env | map(select(.name=="ADVERTISE_IP")) | .[0] | .valueFrom.fieldRef.fieldPath'  |
      tee /dev/stderr)
  [ "${actual}" = "status.hostIP" ]
}

@test "client/DaemonSet: client doesn't expose hostPorts when client.exposeGossipPorts=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'server.enabled=true' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers  | map(select(.name=="consul")) | .[0].ports | map(select(.containerPort==8301)) | .[0].hostPort'  |
      tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "client/DaemonSet: client exposes hostPorts when client.exposeGossipPorts=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.enabled=true' \
      --set 'client.exposeGossipPorts=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers  | map(select(.name=="consul")) | .[0].ports | map(select(.containerPort==8301)) | .[0].hostPort'  |
      tee /dev/stderr)
  [ "${actual}" = "8301" ]
}

#--------------------------------------------------------------------
# dataDirectoryHostPath

@test "client/DaemonSet: data directory is emptyDir by defaut" {
  cd `chart_dir`
  # Test that hostPath is set to null.
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[0].hostPath == null' | tee /dev/stderr )
  [ "${actual}" = "true" ]

  # Test that emptyDir is set instead.
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[0].emptyDir == {}' | tee /dev/stderr )
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: hostPath data directory can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'client.dataDirectoryHostPath=/opt/consul' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.volumes[0].hostPath.path == "/opt/consul"' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# dnsPolicy

@test "client/DaemonSet: dnsPolicy not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
      . | tee /dev/stderr |
      yq '.spec.template.spec.dnsPolicy == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: dnsPolicy can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
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
      . | tee /dev/stderr |
      yq '.spec.template.spec.hostNetwork == null' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/DaemonSet: hostNetwork can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml \
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
      --set "client.updateStrategy=${updateStrategy}" \
      . | tee /dev/stderr | \
      yq -c '.spec.updateStrategy == {"type":"RollingUpdate","rollingUpdate":{"maxUnavailable":5}}' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.openshift.enabled & client.securityContext

@test "client/DaemonSet: securityContext is not set when global.openshift.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-daemonset.yaml  \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

#--------------------------------------------------------------------
# client.securityContext

@test "client/DaemonSet: sets default security context settings" {
  cd `chart_dir`
  local security_context=$(helm template \
      -s templates/client-daemonset.yaml  \
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
      --set 'client.securityContext.runAsNonRoot=false' \
      --set 'client.securityContext.privileged=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)

  local actual=$(echo $security_context | jq -r .runAsNonRoot)
  [ "${actual}" = "false" ]

  local actual=$(echo $security_context | jq -r .privileged)
  [ "${actual}" = "true" ]
}
