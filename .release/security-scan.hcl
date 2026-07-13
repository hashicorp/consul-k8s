# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

# These scan results are run as part of CRT workflows.

# Un-triaged results will block release. See `security-scanner` docs for more
# information on how to add `triage` config to unblock releases for specific results.
# In most cases, we should not need to disable the entire scanner to unblock a release.

# To run manually, install scanner and then from the repository root run
# `SECURITY_SCANNER_CONFIG_FILE=.release/security-scan.hcl scan ...`
# To scan a local container, add `local_daemon = true` to the `container` block below.
# See `security-scanner` docs or run with `--help` for scan target syntax.

container {
  dependencies    = true
  alpine_security = true
  osv             = true
  go_modules      = true

  secrets {
    all = true
  }

  triage {
    suppress {
      vulnerabilites = [
        "GO-2026-5932", // Fix not available yet
        "ALPINE-CVE-2021-42376", // False positive in Alpine Linux's busybox@1.37.0-r31
        // The scanner is flagging this CVE for busybox@1.37.0-r31, but according to NVD - CVE-2021-42376,
        // this version is not affected by the vulnerability. Hence suppressing it for now.
        // Similarly below are list of false positive CVE's flagged by the scanner for Alpine Linux's openssl@3.5.7-r0:
        "ALPINE-CVE-2023-0466",
        "ALPINE-CVE-2022-20683",
        "ALPINE-CVE-2023-4807",
        "ALPINE-CVE-2022-1292",
        "ALPINE-CVE-2022-2068",
        // All the above false positives must be discussed with security team and removed from the list once they are fixed in the scanner.
      ]
      paths = [
        // The OSV scanner will trip on several packages that are included in the
        // the UBI images. This is due to RHEL using the same base version in the
        // package name for the life of the distro regardless of whether or not
        // that version has been patched for security. Rather than enumate ever
        // single CVE that the OSV scanner will find (several tens) we'll ignore
        // the base UBI packages.
        "usr/lib/sysimage/rpm/*",
        "var/lib/rpm/*",
      ]
    }
  }
}

binary {
  go_modules = true
  osv        = true

  secrets {
    all = true
  }

  triage {
    suppress {
      vulnerabilities = [
        "GO-2026-5622", // Fix not available yet
        "GO-2026-5932", // Fix not available yet
        "GO-2026-5064", // Fix not available yet
        "GO-2026-5338", // Fix not available yet
      ]
    }
  }
}

repository {
  go_modules = true
  osv        = true

  triage {
    suppress {
      vulnerabilities = []
    }
  }
}
