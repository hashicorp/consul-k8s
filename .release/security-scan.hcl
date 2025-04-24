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
  dependencies = true
  alpine_secdb = true

  secrets {
    all = true
  }
}

binary {
  go_modules   = true
  osv          = true

  secrets {
    all = true
  }

  triage {
    suppress {
      vulnerabilites = [
        # NET-8174 (2024-02-20): Chart YAML path traversal (not impacted)
        "GHSA-v53g-5gjp-272r", 
        "GO-2024-2554", # alias
        "CVE-2024-25620", # alias
        # NET-8174 (2024-02-26): Missing YAML Content Leads To Panic (requires malicious plugin)
        "GHSA-r53h-jv2g-vpx6", 
        "CVE-2024-26147", # alias
        "GHSA-jw44-4f3j-q396", # Tracked in NET-8174
        "CVE-2019-25210",
        "GO-2022-0635",
        "CVE-2025-46394",
        "CVE-2024-58251"
      ]
    }
  }
}
