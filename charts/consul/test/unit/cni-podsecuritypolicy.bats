#!/usr/bin/env bats

load _helpers

@test "cni/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-podsecuritypolicies.yaml  \
      .
}

@test "cni/PodSecurityPolicy: disabled with cni disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-podsecuritypolicy.yaml  \
      --set 'connectInject.cni.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "cni/PodSecurityPolicy: enabled with connectInject.cni.enabled=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-podsecuritypolicy.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == "true" ]]
}

