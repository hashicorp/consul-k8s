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
        "CVE-2025-58181",
        "CVE-2025-47914",
        "GO-2022-0635",
        "CVE-2025-7425",
        "CVE-2022-29458",
        "CVE-2025-6965",
        "CVE-2025-6395",
        "CVE-2024-12797",
        "CVE-2025-5702",
        "CVE-2025-8058",
        "CVE-2024-4067",
        "CVE-2025-31115",
        "CVE-2025-3576",
        "CVE-2025-6021",
        "CVE-2025-25724",
        "CVE-2024-57970",
        "CVE-2025-32414",
        "CVE-2024-52533",
        "CVE-2025-5914",
        "CVE-2025-3277",
        "CVE-2024-40896",
        #  Dependency Scanner
        "DLA-3972-1", # var/lib/dpkg/status.d/tzdata:
        "DLA-4085-1",
        "DLA-4105-1",
        "DLA-4403-1",
        "DEBIAN-CVE-2023-5678", # var/lib/dpkg/status.d/openssl:
        "DEBIAN-CVE-2024-0727",
        "DEBIAN-CVE-2024-2511",
        "DEBIAN-CVE-2024-4741",
        "DEBIAN-CVE-2024-5535",
        "DEBIAN-CVE-2024-9143",
        "DEBIAN-CVE-2024-13176",
        "DEBIAN-CVE-2025-9230",
        "DEBIAN-CVE-2025-27587",
        "DLA-3942-2",
        "DLA-4176-1",
        "DLA-4321-1",
        # Go Modules Scanner usr/local/bin/discover
        "GHSA-4f99-4q7p-p3gh",
        "GO-2025-4116",
        "GO-2025-4134",
        "GO-2025-4135",
        "GO-2025-4188",
        "GHSA-f6x5-jh6r-wrfv",
        "GHSA-j5w8-q4qc-rx2x",
        # Dependency Scanner var/lib/rpm/rpmdb.sqlite:1:1
        "CVE-2006-1174",
        "CVE-2010-5298",
        "CVE-2014-3505",
        "CVE-2014-3513",
        "CVE-2014-3570",
        "CVE-2014-8176",
        "CVE-2015-0209",
        "CVE-2015-3194",
        "CVE-2015-3197",
        "CVE-2015-4000",
        "CVE-2015-7575",
        "CVE-2016-0799",
        "CVE-2016-2177",
        "CVE-2016-7056",
        "CVE-2016-8610",
        "CVE-2017-3735",
        "CVE-2017-3736",
        "CVE-2018-0734",
        "CVE-2018-0735",
        "CVE-2019-1547",
        "CVE-2019-1551",
        "CVE-2020-1971",
        "CVE-2021-23840",
        "CVE-2021-3449",
        "CVE-2021-3712",
        "CVE-2021-43618",
        "CVE-2022-0778",
        "CVE-2022-1292",
        "CVE-2022-3358",
        "CVE-2022-3602",
        "CVE-2022-4203",
        "CVE-2022-4304",
        "CVE-2023-0286",
        "CVE-2023-0464",
        "CVE-2023-2975",
        "CVE-2023-3446",
        "CVE-2023-4641",
        "CVE-2023-5363",
        "CVE-2024-2511",
        "CVE-2024-5535",
        "CVE-2024-6119",
        "CVE-2024-56433",
        "CVE-2025-4598",
        "CVE-2025-9086",
        "CVE-2025-9230",
        "CVE-2025-9714",
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
