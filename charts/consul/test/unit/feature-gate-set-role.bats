#!/usr/bin/env bats

load _helpers

@test "feature-gate-set/RBAC: Kubernetes discovery grants only pod-list access in the release namespace" {
  cd `chart_dir`
  local actual=$(helm template discovery . --namespace discovery-ns \
      -s templates/feature-gate-set-role.yaml \
      --set server.enabled=false \
      --set externalServers.enabled=true \
      --set 'externalServers.hosts[0]=provider=k8s namespace=discovery-ns' |
      yq -c '{kind, namespace: .metadata.namespace, rules}')
  [ "$actual" = '{"kind":"Role","namespace":"discovery-ns","rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["list"]}]}' ]
}

@test "feature-gate-set/RBAC: binds the chart hook service account" {
  cd `chart_dir`
  local actual=$(helm template discovery . --namespace discovery-ns \
      -s templates/feature-gate-set-rolebinding.yaml \
      --set server.enabled=false \
      --set externalServers.enabled=true \
      --set 'externalServers.hosts[0]=provider=k8s namespace=discovery-ns' |
      yq -c '{roleRef, subjects}')
  [ "$actual" = '{"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"Role","name":"discovery-consul-feature-gate-set"},"subjects":[{"kind":"ServiceAccount","name":"discovery-consul-feature-gate-set","namespace":"discovery-ns"}]}' ]
}

@test "feature-gate-set/RBAC: hooks run before the Job on install and upgrade" {
  cd `chart_dir`
  for template in role rolebinding; do
    local actual=$(helm template . \
        -s templates/feature-gate-set-${template}.yaml \
        --set server.enabled=false \
        --set externalServers.enabled=true \
        --set 'externalServers.hosts[0]=provider="k8s" namespace=default' |
        yq -c '.metadata.annotations')
    [ "$actual" = '{"helm.sh/hook":"post-install,post-upgrade","helm.sh/hook-weight":"0","helm.sh/hook-delete-policy":"hook-succeeded,before-hook-creation"}' ]
  done
}

@test "feature-gate-set/RBAC: no discovery permissions for DNS IP exec or other providers" {
  cd `chart_dir`
  for host in consul.example.com 192.0.2.1 'exec=echo 192.0.2.1' 'exec=echo provider=k8s' 'provider=aws region=us-east-1' 'provider=k8s-other'; do
    for template in role rolebinding; do
      assert_empty helm template . \
          -s templates/feature-gate-set-${template}.yaml \
          --set server.enabled=false \
          --set externalServers.enabled=true \
          --set "externalServers.hosts[0]=$host"
    done
  done
}

@test "feature-gate-set/RBAC: not rendered without external servers" {
  cd `chart_dir`
  for template in role rolebinding; do
    assert_empty helm template . -s templates/feature-gate-set-${template}.yaml
  done
}
