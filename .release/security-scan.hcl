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

  triage {
    suppress {
      vulnerabilities = [
        "CVE-2024-58251",  # busybox@1.37.0-r19 - Alpine Linux security issue
        "CVE-2025-46394",  # busybox@1.37.0-r19 - Alpine Linux security issue
        "CVE-2025-47268",  # iputils@20240905-r0 - Alpine Linux security issue
        "CVE-2025-48964"   # iputils@20240905-r0 - Alpine Linux security issue
      ]
    }
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
      vulnerabilities = [
        "GO-2022-0635",
        "GO-2022-0646"
      ]
    }
  }
}

repository {
  go_modules = true
  osv        = true

  triage {
    suppress {
      vulnerabilities = [
        "GO-2022-0635",
        "GO-2022-0646"
      ]
    }
  }
}
