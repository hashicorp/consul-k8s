# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

# Configuration for security scanner.
# Run on PRs and pushes to `main` and `release/**` branches.
# See .github/workflows/security-scan.yml for CI config.

# To run manually, install scanner and then run `scan repository .`

# Scan results are triaged via the GitHub Security tab for this repo.
# See `security-scanner` docs for more information on how to add `triage` config
# for specific results or to exclude paths.

# .release/security-scan.hcl controls scanner config for release artifacts, which
# unlike the scans configured here, will block releases in CRT.

repository {
  go_modules   = true
  npm          = true
  osv          = true

  secrets {
    all = true
  }

  triage {
    suppress {
      paths = [
        # Ignore test and local tool modules, which are not included in published
        # artifacts.
        "acceptance/*",
        "hack/*",
      ]
      vulnerabilites = [
        # NET-8174 (2024-02-20): Chart YAML path traversal (not impacted)
        "GHSA-v53g-5gjp-272r", # alias CVE-2024-25620
        # NET-8174 (2024-02-26): Missing YAML Content Leads To Panic (requires malicious plugin)
        "GHSA-r53h-jv2g-vpx6", # alias CVE-2024-26147
      ]
    }
  }
}
