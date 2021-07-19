#!/usr/bin/env bats

load _helpers

@test "connectInject/MutatingWebhookConfiguration: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-mutatingwebhook.yaml  \
      .
}

@test "connectInject/MutatingWebhookConfiguration: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-mutatingwebhook.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/MutatingWebhookConfiguration: disable with connectInject.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-mutatingwebhook.yaml  \
      --set 'connectInject.enabled=false' \
      .
}

@test "connectInject/MutatingWebhookConfiguration: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-mutatingwebhook.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "connectInject/MutatingWebhookConfiguration: namespace is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-mutatingwebhook.yaml  \
      --set 'connectInject.enabled=true' \
      --namespace foo \
      . | tee /dev/stderr |
      yq '.webhooks[0].clientConfig.service.namespace' | tee /dev/stderr)
  [ "${actual}" = "\"foo\"" ]
}
