#!/usr/bin/env bats

load _helpers

@test "server/ExposeServicePerReplica: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      .
}

@test "server/ExposeServicePerReplica: disabled with server.exposeServicePerReplica.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=false' \
      .
}

@test "server/ExposeServicePerReplica: disabled when server.enabled is false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.enabled=false' \
      --set 'server.exposeServicePerReplica.enabled=true' \
      .
}

@test "server/ExposeServicePerReplica: creates one Service per replica" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      grep -c "^kind: Service")
  [ "${actual}" = "3" ]
}

@test "server/ExposeServicePerReplica: names Services by ordinal" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-server-0-lb" ]

  actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==2) | .metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-server-2-lb" ]
}

@test "server/ExposeServicePerReplica: selector pins to a single pod ordinal" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==1) | .spec.selector["statefulset.kubernetes.io/pod-name"]' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-server-1" ]
}

@test "server/ExposeServicePerReplica: service type is always LoadBalancer" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.type' | tee /dev/stderr)
  [ "${actual}" = "LoadBalancer" ]
}

@test "server/ExposeServicePerReplica: exposes serflan tcp+udp and rpc ports by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[] | select(.name == "serflan-tcp") | .port' | tee /dev/stderr)
  [ "${actual}" = "8301" ]

  actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[] | select(.name == "serflan-tcp") | .protocol' | tee /dev/stderr)
  [ "${actual}" = "TCP" ]

  actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[] | select(.name == "serflan-udp") | .port' | tee /dev/stderr)
  [ "${actual}" = "8301" ]

  actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[] | select(.name == "serflan-udp") | .protocol' | tee /dev/stderr)
  [ "${actual}" = "UDP" ]

  actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[] | select(.name == "rpc") | .port' | tee /dev/stderr)
  [ "${actual}" = "8300" ]
}

@test "server/ExposeServicePerReplica: port protocol can be set on an overridden port" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.ports[0].name=custom-udp' \
      --set 'server.exposeServicePerReplica.ports[0].port=9000' \
      --set 'server.exposeServicePerReplica.ports[0].protocol=UDP' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[0].protocol' | tee /dev/stderr)
  [ "${actual}" = "UDP" ]
}

@test "server/ExposeServicePerReplica: ports can be fully overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.ports[0].name=https' \
      --set 'server.exposeServicePerReplica.ports[0].port=8501' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]

  actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.ports[0].name=https' \
      --set 'server.exposeServicePerReplica.ports[0].port=8501' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[0].targetPort' | tee /dev/stderr)
  [ "${actual}" = "8501" ]
}

@test "server/ExposeServicePerReplica: port targetPort can differ from port" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.ports[0].name=custom' \
      --set 'server.exposeServicePerReplica.ports[0].port=9000' \
      --set 'server.exposeServicePerReplica.ports[0].targetPort=9999' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[0].targetPort' | tee /dev/stderr)
  [ "${actual}" = "9999" ]
}

@test "server/ExposeServicePerReplica: per-port nodePort is never rendered" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.ports[0].name=serflan' \
      --set 'server.exposeServicePerReplica.ports[0].port=8301' \
      --set 'server.exposeServicePerReplica.ports[0].nodePort=30301' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .spec.ports[0].nodePort' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/ExposeServicePerReplica: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/ExposeServicePerReplica: can set static annotations" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.annotations=key: value' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .metadata.annotations.key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "server/ExposeServicePerReplica: annotations are templated per-replica with .index" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.replicas=2' \
      --set-string 'server.exposeServicePerReplica.annotations=external-dns.alpha.kubernetes.io/hostname: consul-server-{{ .index }}.example.com' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==0) | .metadata.annotations["external-dns.alpha.kubernetes.io/hostname"]' | tee /dev/stderr)
  [ "${actual}" = "consul-server-0.example.com" ]

  actual=$(helm template \
      -s templates/server-expose-service-per-replica.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.replicas=2' \
      --set-string 'server.exposeServicePerReplica.annotations=external-dns.alpha.kubernetes.io/hostname: consul-server-{{ .index }}.example.com' \
      . | tee /dev/stderr |
      yq -N 'select(documentIndex==1) | .metadata.annotations["external-dns.alpha.kubernetes.io/hostname"]' | tee /dev/stderr)
  [ "${actual}" = "consul-server-1.example.com" ]
}

@test "server/ExposeServicePerReplica: advertise defaults to false, pod IP still advertised" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      grep -c "name: lb-advertise-addr-init" || true)
  [ "${actual}" = "0" ]

  actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      grep -c -- '-advertise="${ADVERTISE_IP}"')
  [ "${actual}" = "1" ]
}

@test "server/ExposeServicePerReplica: advertise=true adds lb-advertise-addr-init and omits -advertise flag" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.advertise=true' \
      . | tee /dev/stderr |
      grep -c "name: lb-advertise-addr-init")
  [ "${actual}" = "1" ]

  actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.advertise=true' \
      . | tee /dev/stderr |
      grep -c -- '-advertise="${ADVERTISE_IP}"' || true)
  [ "${actual}" = "0" ]
}

@test "server/ExposeServicePerReplica: advertise=true has no effect when exposeServicePerReplica is disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=false' \
      --set 'server.exposeServicePerReplica.advertise=true' \
      . | tee /dev/stderr |
      grep -c "name: lb-advertise-addr-init" || true)
  [ "${actual}" = "0" ]

  actual=$(helm template \
      -s templates/server-statefulset.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=false' \
      --set 'server.exposeServicePerReplica.advertise=true' \
      . | tee /dev/stderr |
      grep -c -- '-advertise="${ADVERTISE_IP}"')
  [ "${actual}" = "1" ]
}

@test "server/ExposeServicePerReplica: RBAC Role grants services get only when advertise is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-role.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      . | tee /dev/stderr |
      grep -c '"services"' || true)
  [ "${actual}" = "0" ]

  actual=$(helm template \
      -s templates/server-role.yaml \
      --kube-version "1.22" \
      --set 'server.exposeServicePerReplica.enabled=true' \
      --set 'server.exposeServicePerReplica.advertise=true' \
      . | tee /dev/stderr |
      grep -c '"services"')
  [ "${actual}" = "1" ]
}
