# HC-COMPUTE-010 / SECVULN-44200 - install hc-security-base (IBM CISO baseline).
#
# This runs as node pre_userdata (before /etc/eks/bootstrap.sh) on a Canonical
# Ubuntu EKS AMI. It replicates the runtime subset of
# ami-builder/scripts/packer-install_meta_deb.sh (enable internal Artifactory apt
# repo, then install the package) so the compliance scanner finds hc-security-base
# in the node package database. It deliberately does NOT run the heavy base-image
# pruning that the Packer build does, to avoid disrupting a live EKS worker node.
#
# Failures are non-fatal: the node still joins the cluster (so CI is not blocked)
# but the failure is logged for investigation.
install_hc_security_base() {
  export DEBIAN_FRONTEND=noninteractive

  local AFY_USER='${afy_user}'
  local AFY_PASSWORD='${afy_password}'
  local BASE_URL="https://artifactory.hashicorp.engineering/artifactory"
  local AUTH_FILE="/etc/apt/auth.conf.d/hc_artifactory.conf"
  local SRC_FILE="/etc/apt/sources.list.d/hc_artifactory.list"
  local GPG_FILE="/etc/apt/trusted.gpg.d/hc_artifactory.asc"
  local CODENAME
  CODENAME="$(lsb_release -cs)"

  if [ -z "$AFY_USER" ] || [ -z "$AFY_PASSWORD" ]; then
    echo "hc-security-base: Artifactory credentials not provided; skipping install"
    return 1
  fi

  # Internal Artifactory apt auth (mode 0600, root-owned - holds credentials).
  install -m 0600 -o root -g root /dev/null "$AUTH_FILE"
  echo "machine artifactory.hashicorp.engineering login $AFY_USER password $AFY_PASSWORD" > "$AUTH_FILE"

  printf 'deb [trusted=yes] %s/deb %s main\ndeb [trusted=yes] %s/deb deb main\n' \
    "$BASE_URL" "$CODENAME" "$BASE_URL" > "$SRC_FILE"
  chmod 0644 "$SRC_FILE"

  curl -fsSL -u "$AFY_USER:$AFY_PASSWORD" "$BASE_URL/api/gpg/key/public" -o "$GPG_FILE"

  apt-get -qy update
  apt-get -qy install --no-install-recommends hc-security-base

  # Hygiene: remove the internal repo + credentials from the running node once
  # the package is installed; the compliance check only needs the package present.
  rm -f "$AUTH_FILE" "$SRC_FILE"
  apt-get -qy update || true
}

install_hc_security_base || echo "WARNING: hc-security-base install failed; node will still join the cluster"
