#!/usr/bin/env bats

load _helpers

@test "tlsInit/Job: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-job.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/Job: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/Job: enabled with global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/Job: disabled when server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInit/Job: enabled when global.tls.enabled=true and server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/Job: sets additional IP SANs when provided and global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.serverAdditionalIPSANs[0]=1.1.1.1' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-additional-ipaddress=1.1.1.1"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInit/Job: sets additional DNS SANs when provided and global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.serverAdditionalDNSSANs[0]=example.com' \
      . | tee /dev/stderr |
      yq '.spec.template.spec.containers[0].command | any(contains("-additional-dnsname=example.com"))' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
