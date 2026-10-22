#!/usr/bin/env bats

# Chart gating for the new Consul API Gateway (consul.hashicorp.com Gateway API
# CRDs + custom gateway controller) on OpenShift, including the Helm upgrade flags
# exercised by acceptance/tests/openshift/consul_api_gateway_openshift_test.go.

load _helpers

ocp_template() {
  helm template \
    --set 'connectInject.enabled=true' \
    --set 'global.openshift.enabled=true' \
    --set 'connectInject.apiGateway.manageExternalCRDs=true' \
    "$@" \
    .
}

#--------------------------------------------------------------------
# custom gateway controller flag on connect-injector

@test "consulAPIGateway/OpenShift: custom controller disabled by default" {
  cd `chart_dir`
  run bash -c "$(declare -f ocp_template); ocp_template -s templates/connect-inject-deployment.yaml | grep -c -- '-enable-custom-gateway-crd-controller=true'"
  [ "${output}" = "0" ]
}

@test "consulAPIGateway/OpenShift: custom controller enabled with global.openshift.crds.consulapi.enabled" {
  cd `chart_dir`
  run bash -c "$(declare -f ocp_template); ocp_template -s templates/connect-inject-deployment.yaml --set 'global.openshift.crds.consulapi.enabled=true' | grep -c -- '-enable-custom-gateway-crd-controller=true'"
  [ "${output}" = "1" ]
}

@test "consulAPIGateway/OpenShift: custom controller and TCPRoute watch enabled with upgradeTo419From418 and enableTcpRoute" {
  cd `chart_dir`
  local out=$(ocp_template -s templates/connect-inject-deployment.yaml \
    --set 'global.openshift.upgradeTo419From418=true' \
    --set 'global.openshift.crds.enableTcpRoute=true')
  echo "$out" | grep -q -- '-enable-custom-gateway-crd-controller=true'
  echo "$out" | grep -q -- '-enabe-tcp-route=true'
}

@test "consulAPIGateway/OpenShift: upgradeTo419From418 without enableTcpRoute does not enable custom controller" {
  cd `chart_dir`
  run bash -c "$(declare -f ocp_template); ocp_template -s templates/connect-inject-deployment.yaml --set 'global.openshift.upgradeTo419From418=true' | grep -c -- '-enable-custom-gateway-crd-controller=true'"
  [ "${output}" = "0" ]
}

@test "consulAPIGateway/OpenShift: isOcpGreaterthan4_18 with enableTcpRoute enables custom controller but not legacy TCPRoute watch" {
  cd `chart_dir`
  local out=$(ocp_template -s templates/connect-inject-deployment.yaml \
    --set 'global.openshift.isOcpGreaterthan4_18=true' \
    --set 'global.openshift.crds.enableTcpRoute=true')
  echo "$out" | grep -q -- '-enable-custom-gateway-crd-controller=true'
  local tcp=$(echo "$out" | grep -c -- '-enabe-tcp-route=true' || true)
  [ "${tcp}" = "0" ]
}

#--------------------------------------------------------------------
# CRDs

@test "consulAPIGateway/OpenShift: consul.hashicorp.com Gateway CRD not rendered by default" {
  cd `chart_dir`
  run bash -c "$(declare -f ocp_template); ocp_template -s templates/consul-crd-gateways-external.yaml"
  [ "$status" -ne 0 ]
}

@test "consulAPIGateway/OpenShift: consul.hashicorp.com Gateway API CRDs rendered with global.openshift.crds.consulapi.enabled" {
  cd `chart_dir`
  local out=$(ocp_template --set 'global.openshift.crds.consulapi.enabled=true')
  for crd in cgateways chttproutes creferencegrants customgatewayclasses ctcproutes; do
    echo "$out" | grep -q "name: ${crd}.consul.hashicorp.com"
  done
}

@test "consulAPIGateway/OpenShift: gateway.networking.k8s.io CRDs kept when upgrading from 4.18 to 4.19" {
  cd `chart_dir`
  local out=$(ocp_template \
    --set 'global.openshift.upgradeTo419From418=true' \
    --set 'global.openshift.crds.enableTcpRoute=true')
  echo "$out" | grep -q 'name: gateways.gateway.networking.k8s.io'
  echo "$out" | grep -q 'name: cgateways.consul.hashicorp.com'
}

@test "consulAPIGateway/OpenShift: gateway.networking.k8s.io CRDs not rendered when isOcpGreaterthan4_18" {
  cd `chart_dir`
  run bash -c "$(declare -f ocp_template); ocp_template --set 'global.openshift.isOcpGreaterthan4_18=true' | grep -c 'name: gateways.gateway.networking.k8s.io'"
  [ "${output}" = "0" ]
}

#--------------------------------------------------------------------
# gateway-resources-custom hook (creates consul-custom-class)

@test "consulAPIGateway/OpenShift: gateway-resources-custom Job not rendered by default" {
  cd `chart_dir`
  run bash -c "$(declare -f ocp_template); ocp_template -s templates/gateway-resources-job-custom.yaml"
  [ "$status" -ne 0 ]
}

@test "consulAPIGateway/OpenShift: gateway-resources-custom Job creates consul-custom-class for the custom controller" {
  cd `chart_dir`
  local out=$(ocp_template -s templates/gateway-resources-job-custom.yaml --set 'global.openshift.crds.consulapi.enabled=true')
  echo "$out" | grep -q -- '- -gateway-class-name=consul-custom-class'
  echo "$out" | grep -q -- '- -controller-name=consul.hashicorp.com/gateway-controller-consul'
  echo "$out" | grep -q -- '- -openshift-scc-name=restricted-v2'
  echo "$out" | grep -q '"helm.sh/hook": post-install,post-upgrade'
}

#--------------------------------------------------------------------
# generate-manifests upgrade hooks

@test "consulAPIGateway/OpenShift: generate-manifests Job runs pre-upgrade with consulapi-enabled when upgrading from 4.18 to 4.19" {
  cd `chart_dir`
  local out=$(ocp_template -s templates/generate-manifests-job.yaml \
    --set 'global.generateManifests=true' \
    --set 'global.openshift.upgradeTo419From418=true' \
    --set 'global.openshift.crds.enableTcpRoute=true')
  echo "$out" | grep -q '"helm.sh/hook": pre-upgrade'
  echo "$out" | grep -q -- '- -consulapi-enabled=true'
}

@test "consulAPIGateway/OpenShift: generate-manifests Job does not migrate to consul.hashicorp.com without consulapi flags" {
  cd `chart_dir`
  run bash -c "$(declare -f ocp_template); ocp_template -s templates/generate-manifests-job.yaml --set 'global.generateManifests=true' | grep -c -- '-consulapi-enabled=true'"
  [ "${output}" = "0" ]
}

@test "consulAPIGateway/OpenShift: apply-manifests Job uses oc and applies consulapi manifests post-upgrade" {
  cd `chart_dir`
  local out=$(ocp_template -s templates/generate-manifests-apply-job.yaml --set 'global.generateManifests=true')
  echo "$out" | grep -q '"helm.sh/hook": post-upgrade'
  echo "$out" | grep -q 'registry.redhat.io/openshift4/ose-cli'
  echo "$out" | grep -q 'oc apply -R -f /consul/config/consulapi'
}
