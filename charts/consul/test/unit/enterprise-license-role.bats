#!/usr/bin/env bats

load _helpers

@test "enterpriseLicense/Role: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-role.yaml  \
      .
}

@test "enterpriseLicense/Role: disabled if autoload is true (default)" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-role.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      .
}

@test "enterpriseLicense/Role: disabled with server=false, ent secret defined" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-role.yaml  \
      --set 'server.enabled=false' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/Role: disabled when ent secretName missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-role.yaml  \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/Role: disabled when ent secretKey missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-role.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/Role: enabled when ent license defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-role.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "enterpriseLicense/Role: rules are empty if global.acls.manageSystemACLs and global.enablePodSecurityPolicies are false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-role.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      . | tee /dev/stderr |
      yq '.rules | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "enterpriseLicense/Role: allows acl token when global.acls.manageSystemACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-role.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resourceNames[0] == "RELEASE-NAME-consul-enterprise-license-acl-token")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "enterpriseLicense/Role: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-role.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
