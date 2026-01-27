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
        "GO-2022-0635",
        "GHSA-f6x5-jh6r-wrfv",
        "GHSA-4f99-4q7p-p3gh",
        "GO-2025-4135",
        "GHSA-j5w8-q4qc-rx2x",
        "GO-2025-4188",
        "GO-2025-4134",
        "GO-2025-4116"
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
