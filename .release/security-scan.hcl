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
        "CVE-2024-58251",
        "CVE-2025-46394",
        "CVE-2025-48964",
        "CVE-2025-47268",
        "GO-2022-0635",
        "CVE-2025-6021",
        "CVE-2024-40896",
        "CVE-2024-52533",
        "CVE-2024-57970",
        "CVE-2024-12797",
        "CVE-2024-4067",
        "CVE-2025-7425",
        "CVE-2025-3277",
        "CVE-2022-29458",
        "CVE-2025-5914",
        "CVE-2025-31115",
        "CVE-2025-5702",
        "CVE-2025-8058",
        "CVE-2025-25724",
        "CVE-2025-6395",
        "CVE-2025-6965",
        "CVE-2025-3576",
        "CVE-2025-32414"
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
      vulnerabilites = [
        "GO-2022-0635"
      ]
    }
  }
}
