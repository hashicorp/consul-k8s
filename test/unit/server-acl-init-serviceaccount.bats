#!/usr/bin/env bats

load _helpers

@test "serverACLInit/ServiceAccount: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/ServiceAccount: enabled with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-serviceaccount.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/ServiceAccount: disabled with server=false and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-serviceaccount.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/ServiceAccount: enabled with client=false and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-serviceaccount.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/ServiceAccount: enabled with externalServers.enabled=true and global.acls.manageSystemACLs=true, but server.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-serviceaccount.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.https.address=foo.com' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/ServiceAccount: fails if both externalServers.enabled=true and server.enabled=true" {
  cd `chart_dir`
  run helm template \
      -x templates/server-acl-init-serviceaccount.yaml  \
      --set 'server.enabled=true' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "only one of server.enabled or externalServers.enabled can be set" ]]
}

@test "serverACLInit/ServiceAccount: fails if both externalServers.enabled=true and server.enabled not set to false" {
  cd `chart_dir`
  run helm template \
      -x templates/server-acl-init-serviceaccount.yaml \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "only one of server.enabled or externalServers.enabled can be set" ]]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "serverACLInit/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/server-acl-init-serviceaccount.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.imagePullSecrets[0].name=my-secret' \
      --set 'global.imagePullSecrets[1].name=my-secret2' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}
