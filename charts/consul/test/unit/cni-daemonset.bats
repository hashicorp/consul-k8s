#!/usr/bin/env bats

load _helpers

@test "cni/DaemonSet: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-daemonset.yaml  \
      .
}

@test "cni/DaemonSet: enabled with connectInject.cni.enabled=true and connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == "true" ]]
}

@test "cni/DaemonSet: disabled with connectInject.cni.enabled=false and connectInject.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      --set 'connectInject.cni.enabled=false' \
      --set 'connectInject.enabled=true' \
      -s templates/cni-daemonset.yaml  \
      .
}

@test "cni/DaemonSet: throws error when connectInject.enabled=true and connectInject.enabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=false' \
      -s templates/cni-daemonset.yaml  \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.enabled must be true if connectInject.cni.enabled is true" ]]
}

@test "cni/DaemonSet: image defaults to global.imageK8S" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.imageK8S=foo' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].image' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

@test "cni/Daemonset: all command arguments" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/cni-daemonset.yaml \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.multus=foo' \
      --set 'connectInject.cni.logLevel=bar' \
      --set 'connectInject.cni.cniBinDir=baz' \
      --set 'connectInject.cni.cniNetDir=foo' \
      bar \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq 'any(contains("consul-k8s-control-plane"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("install-cni"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("multus=foo"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("log-level=bar"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("cni-bin-dir=baz"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("cni-net-dir=foo"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]

  local actual=$(echo "$cmd" |
    yq 'any(contains("dns-prefix=bar-consul"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# updateStrategy

@test "cni/DaemonSet: no updateStrategy by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.updateStrategy' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "cni/DaemonSet: updateStrategy can be set" {
  cd `chart_dir`
  local updateStrategy="type: RollingUpdate
rollingUpdate:
  maxUnavailable: 5
"
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set "connectInject.cni.updateStrategy=${updateStrategy}" \
      . | tee /dev/stderr | \
      yq -c '.spec.updateStrategy == {"type":"RollingUpdate","rollingUpdate":{"maxUnavailable":5}}' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}


#--------------------------------------------------------------------
# resources

@test "cni/DaemonSet: resources defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.template.spec.containers[0].resources' | tee /dev/stderr)
  [ "${actual}" = '{"limits":{"cpu":"50m","memory":"50Mi"},"requests":{"cpu":"50m","memory":"50Mi"}}' ]
}

@test "cni/DaemonSet: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.resources.foo=bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers[0].resources.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# securityContext

@test "cni/DaemonSet: securityContext is not set when global.openshift.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "cni/DaemonSet: sets default security context settings" {
  cd `chart_dir`
  local security_context=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)

  local actual=$(echo $security_context | jq -r .runAsNonRoot)
  [ "${actual}" = "false" ]

  local actual=$(echo $security_context | jq -r .runAsUser)
  [ "${actual}" = "0" ]

  local actual=$(echo $security_context | jq -r .runAsGroup)
  [ "${actual}" = "0" ]
}

@test "cni/DaemonSet: can overwrite security context settings" {
  cd `chart_dir`
  local security_context=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.securityContext.runAsNonRoot=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext' | tee /dev/stderr)

  local actual=$(echo $security_context | jq -r .runAsNonRoot)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# volumes

@test "cni/DaemonSet: sets default cni-bin-dir volume hostPath" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "cni-bin-dir")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"cni-bin-dir","hostPath":{"path":"/opt/cni/bin"}}' ]
}

@test "cni/DaemonSet: sets default cni-net-dir volume hostPath" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "cni-net-dir")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"cni-net-dir","hostPath":{"path":"/etc/cni/net.d"}}' ]
}

@test "cni/DaemonSet: can overwrite default cni-bin-dir volume hostPath" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.cniBinDir=foo' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "cni-bin-dir")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"cni-bin-dir","hostPath":{"path":"foo"}}' ]
}

@test "cni/DaemonSet: can overwrite cni-net-dir volume hostPath" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.cniNetDir=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.volumes[] | select(.name == "cni-net-dir")' | tee /dev/stderr)
      [ "${actual}" = '{"name":"cni-net-dir","hostPath":{"path":"bar"}}' ]
}

#--------------------------------------------------------------------
# volumeMounts

@test "cni/DaemonSet: sets default host cni-bin-dir volumeMount" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "cni-bin-dir")' | tee /dev/stderr)
      [ "${actual}" = '{"mountPath":"/opt/cni/bin","name":"cni-bin-dir"}' ]
}

@test "cni/DaemonSet: sets default host cni-net-dir volumeMount" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "cni-net-dir")' | tee /dev/stderr)
      [ "${actual}" = '{"mountPath":"/etc/cni/net.d","name":"cni-net-dir"}' ]
}

@test "cni/DaemonSet: can overwrite host cni-bin-dir volumeMount" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.cniBinDir=foo' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "cni-bin-dir")' | tee /dev/stderr)
      [ "${actual}" = '{"mountPath":"foo","name":"cni-bin-dir"}' ]
}

@test "cni/DaemonSet: can overwrite host cni-net-dir volumeMount" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.cniNetDir=bar' \
      . | tee /dev/stderr |
      yq -r -c '.spec.template.spec.containers[0].volumeMounts[] | select(.name == "cni-net-dir")' | tee /dev/stderr)
      [ "${actual}" = '{"mountPath":"bar","name":"cni-net-dir"}' ]
}

