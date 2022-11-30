#!/usr/bin/env bats

load _helpers

@test "cni/NetworkAttachmentDefinition: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-networkattachmentdefinition.yaml  \
      .
}

@test "cni/NetworkAttachmentDefinition: disabled when cni enabled and multus disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-securitycontextconstraints.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.cni.multus=false' \
      .
}

@test "cni/NetworkAttachmentDefinition: enabled when cni enabled and multus enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-networkattachmentdefinition.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.cni.multus=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "cni/NetworkAttachmentDefinition: config is set" {
  cd `chart_dir`
  local cmd=$(helm template \
      -s templates/cni-networkattachmentdefinition.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.multus=true' \
      --set 'connectInject.cni.logLevel=bar' \
      --set 'connectInject.cni.cniBinDir=baz' \
      --set 'connectInject.cni.cniNetDir=foo' \
      bar \
      . | tee /dev/stderr |
      yq -rc '.spec.config' | tee /dev/stderr)

  local actual=$(echo "$cmd" |
    yq '.log_level' | tee /dev/stderr)
  [ "${actual}" = '"bar"' ]

  local actual=$(echo "$cmd" |
    yq '.cni_bin_dir' | tee /dev/stderr)
  [ "${actual}" = '"baz"' ]

  local actual=$(echo "$cmd" |
    yq '.cni_net_dir' | tee /dev/stderr)
  [ "${actual}" = '"foo"' ]

}

@test "cni/NetworkAttachmentDefinition: cni namespace has a default when not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-networkattachmentdefinition.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.cni.multus=true' \
      . | tee /dev/stderr |
      yq -r -c '.metadata.namespace' | tee /dev/stderr)
  [[ "${actual}" == "default" ]]
}

@test "cni/NetworkAttachmentDefinition: able to set cni namespace" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-networkattachmentdefinition.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.cni.multus=true' \
      --set 'connectInject.cni.namespace=kube-system' \
      . | tee /dev/stderr |
      yq -r -c '.metadata.namespace' | tee /dev/stderr)
  [[ "${actual}" == "kube-system" ]]
}
