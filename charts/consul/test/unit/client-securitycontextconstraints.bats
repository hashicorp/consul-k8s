#!/usr/bin/env bats

load _helpers

@test "client/SecurityContextConstraints: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      .
}

@test "client/SecurityContextConstraints: disabled with client disabled and global.openshift.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      --set 'client.enabled=false' \
      --set 'global.openshift.enabled=true' \
      .
}

@test "client/SecurityContextConstraints: enabled with global.openshift.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SecurityContextConstraints: host ports are allowed by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -c '.allowHostPorts' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}


#--------------------------------------------------------------------
# client.dataDirectoryHostPath

@test "client/SecurityContextConstraints: disallows hostPath volume by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq '.volumes | any(contains("hostPath"))' | tee /dev/stderr)
  [ "${actual}" = 'false' ]
}

@test "client/SecurityContextConstraints: allows hostPath volume when dataDirectoryHostPath is set" {
  cd `chart_dir`
  # Test that hostPath is an allowed volume type.
  local actual=$(helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      --set 'global.openshift.enabled=true' \
      --set 'client.dataDirectoryHostPath=/opt/consul' \
      . | tee /dev/stderr |
      yq '.volumes | any(contains("hostPath"))' | tee /dev/stderr)
  [ "${actual}" = 'true' ]

  # Test that the path we're allowed to write to host path.
  local actual=$(helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      --set 'global.openshift.enabled=true' \
      --set 'client.dataDirectoryHostPath=/opt/consul' \
      . | tee /dev/stderr |
      yq -r '.allowHostDirVolumePlugin' | tee /dev/stderr)
  [ "${actual}" = 'true' ]
}

#--------------------------------------------------------------------
# client.hostNetwork

@test "client/SecurityContextConstraints: enabled with global.openshift.enabled=true and hostNetwork=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      --set 'global.openshift.enabled=true' \
      --set 'client.hostNetwork=true' \
      . | tee /dev/stderr |
      yq '.allowHostNetwork == true' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/SecurityContextConstraints: enabled with global.openshift.enabled=true and default hostNetwork=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-securitycontextconstraints.yaml  \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq '.allowHostNetwork == false' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
