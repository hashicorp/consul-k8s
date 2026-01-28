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
      vulnerabilities = [
        "CVE-2024-58251",  # busybox@1.37.0-r19 - Alpine Linux security issue
        "CVE-2025-46394",  # busybox@1.37.0-r19 - Alpine Linux security issue
        "CVE-2025-47268",  # iputils@20240905-r0 - Alpine Linux security issue
        "CVE-2025-48964",   # iputils@20240905-r0 - Alpine Linux security issue
        "GHSA-f6x5-jh6r-wrfv",
        "GHSA-4f99-4q7p-p3gh",
        "GO-2025-4135",
        "GHSA-j5w8-q4qc-rx2x",
        "GO-2025-4188",
        "GO-2025-4134",
        "GO-2025-4116",
        #var/lib/rpm/rpmdb.sqlite:1:1
        "CVE-2010-5298",
        "CVE-2014-3513",
        "CVE-2014-8176",
        "CVE-2015-3194",
        "CVE-2015-7575",
        "CVE-2016-7056",
        "CVE-2017-3735",
        "CVE-2019-1547",
        "CVE-2021-3712",
        "CVE-2022-0778",
        "CVE-2022-1292",
        "CVE-2023-0286",
        "CVE-2023-0464",
        "CVE-2023-2975",
        "CVE-2023-4641",
        "CVE-2023-5363",
        "CVE-2024-12797",
        "CVE-2025-13601",
        "CVE-2025-4598",
        "CVE-2025-5702",
        "CVE-2025-6021",
        "CVE-2025-6965",
        "CVE-2025-8058"
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
