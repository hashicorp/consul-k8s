#!/usr/bin/env bats

load _helpers

@test "connectInject/multiport conversion: no Job is rendered on install" {
  cd `chart_dir`
  assert_empty helm template \
    -s templates/connect-inject-multiport-conversion-job.yaml \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=TRANSLATE' \
    .
}

@test "connectInject/multiport conversion: NONE renders no Job on upgrade" {
  cd `chart_dir`
  assert_empty helm template \
    --is-upgrade \
    -s templates/connect-inject-multiport-conversion-job.yaml \
    .
}

@test "connectInject/multiport conversion: TRANSLATE renders an explicit post-upgrade Job" {
  cd `chart_dir`
  local object=$(helm template \
    --is-upgrade \
    -s templates/connect-inject-multiport-conversion-job.yaml \
    --set 'connectInject.default=true' \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=TRANSLATE' \
    --set 'connectInject.k8sAllowNamespaces={team-a,team-b}' \
    --set 'connectInject.k8sDenyNamespaces={blocked}' \
    --set 'connectInject.namespaceSelector=environment: production' \
    .)

  [ "$(echo "$object" | yq -r '.kind')" = "Job" ]
  [ "$(echo "$object" | yq -r '.metadata.annotations."helm.sh/hook"')" = "post-upgrade" ]
  [ "$(echo "$object" | yq -r '.metadata.annotations."helm.sh/hook-delete-policy"')" = "before-hook-creation,hook-succeeded" ]
  [ "$(echo "$object" | yq -r '.spec.backoffLimit')" = "3" ]
  [ "$(echo "$object" | yq -r '.spec.template.spec.serviceAccountName')" = "release-name-consul-connect-injector" ]
  [ "$(echo "$object" | yq -r '.spec.template.metadata.annotations."consul.hashicorp.com/connect-inject"')" = "false" ]

  local args=$(echo "$object" | yq -r '.spec.template.spec.containers[0].args[]')
  [[ "$args" == *"-strategy=TRANSLATE"* ]]
  [[ "$args" == *"-default-inject=true"* ]]
  [[ "$args" == *'-namespace-selector={"matchLabels":{"environment":"production"}}'* ]]
  [[ "$args" == *"-allow-k8s-namespace=team-a"* ]]
  [[ "$args" == *"-allow-k8s-namespace=team-b"* ]]
  [[ "$args" == *"-deny-k8s-namespace=blocked"* ]]
}

@test "connectInject/multiport conversion: DECOMMISSION is passed without implicit translation" {
  cd `chart_dir`
  local args=$(helm template \
    --is-upgrade \
    -s templates/connect-inject-multiport-conversion-job.yaml \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=DECOMMISSION' \
    . | yq -r '.spec.template.spec.containers[0].args[]')

  [[ "$args" == *"-strategy=DECOMMISSION"* ]]
  [[ "$args" != *"-strategy=TRANSLATE"* ]]
}

@test "connectInject/multiport conversion: the release namespace is excluded from conversion" {
  cd `chart_dir`
  local args=$(helm template \
    --is-upgrade \
    --namespace consul-ns \
    -s templates/connect-inject-multiport-conversion-job.yaml \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=DECOMMISSION' \
    . | yq -r '.spec.template.spec.containers[0].args[]')

  [[ "$args" == *"-release-namespace=consul-ns"* ]]
}

@test "connectInject/multiport conversion: disabled injector renders no Job" {
  cd `chart_dir`
  assert_empty helm template \
    --is-upgrade \
    -s templates/connect-inject-multiport-conversion-job.yaml \
    --set 'connectInject.enabled=false' \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=TRANSLATE' \
    .
}

@test "connectInject/multiport conversion: an empty namespace allow-list stays deny-all" {
  cd `chart_dir`
  local args=$(helm template \
    --is-upgrade \
    -s templates/connect-inject-multiport-conversion-job.yaml \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=TRANSLATE' \
    --set-json 'connectInject.k8sAllowNamespaces=[]' \
    . | yq -r '.spec.template.spec.containers[0].args[]')

  [[ "$args" == *"-allow-k8s-namespace="* ]]
  [[ "$args" != *"-allow-k8s-namespace=*"* ]]
}

@test "connectInject/multiport conversion: enabled registration rejects conversion strategy" {
  cd `chart_dir`
  run helm template \
    --is-upgrade \
    --set 'connectInject.multiportServiceRegistration.enabled=true' \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=TRANSLATE' \
    .

  [ "$status" -ne 0 ]
  [[ "$output" == *"conversionStrategy must be NONE when multiport service registration is enabled"* ]]
}

@test "connectInject/multiport conversion: invalid strategy fails before upgrade" {
  cd `chart_dir`
  run helm template \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=translate' \
    .

  [ "$status" -ne 0 ]
  [[ "$output" == *"conversionStrategy must be one of NONE, TRANSLATE, or DECOMMISSION"* ]]
}

@test "connectInject/multiport conversion: reuses injector Deployment placement and resource settings" {
  cd `chart_dir`
  local object=$(helm template \
    --is-upgrade \
    -s templates/connect-inject-multiport-conversion-job.yaml \
    --set 'connectInject.multiportServiceRegistration.conversionStrategy=TRANSLATE' \
    --set 'connectInject.resources.requests.cpu=100m' \
    --set 'connectInject.nodeSelector=workload: system' \
    --set 'connectInject.tolerations=- key: dedicated' \
    .)

  [ "$(echo "$object" | yq -r '.spec.template.spec.containers[0].resources.requests.cpu')" = "100m" ]
  [ "$(echo "$object" | yq -r '.spec.template.spec.nodeSelector.workload')" = "system" ]
  [ "$(echo "$object" | yq -r '.spec.template.spec.tolerations[0].key')" = "dedicated" ]
}

@test "connectInject/multiport conversion: injector ClusterRole can list namespaces and update every converted workload kind" {
  cd `chart_dir`
  local object=$(helm template -s templates/connect-inject-clusterrole.yaml .)

  local json=$(echo "$object" | yq -o=json)
  [ "$(echo "$json" | jq '[.rules[] | select(.apiGroups == [""] and (.resources | index("namespaces")) and (.verbs | index("list")))] | length')" -gt 0 ]
  for resource in deployments statefulsets daemonsets; do
    [ "$(echo "$json" | jq --arg r "$resource" '[.rules[] | select(.apiGroups == ["apps"] and (.resources | index($r)) and (.verbs | index("get")) and (.verbs | index("list")) and (.verbs | index("update")))] | length')" -gt 0 ]
  done

  # The stranded-workload scan reads Pods and follows ReplicaSets one hop.
  [ "$(echo "$json" | jq '[.rules[] | select(.apiGroups == [""] and (.resources | index("pods")) and (.verbs | index("list")))] | length')" -gt 0 ]
  [ "$(echo "$json" | jq '[.rules[] | select(.apiGroups == ["apps"] and (.resources | index("replicasets")) and (.verbs | index("get")))] | length')" -gt 0 ]
}
