schema_version = 1

project {
  license        = "MPL-2.0"
  copyright_year = 2018

  # (OPTIONAL) A list of globs that should not have copyright/license headers.
  # Supports doublestar glob patterns for more flexibility in defining which
  # files or folders should be ignored
  header_ignore = [

    # ignoring charts templates as adding copyright headers breaks all tests
    "charts/consul/templates/**",
    # we don't own these and the tool that adds copyright headers breaks them
    "control-plane/config/crd/external/**",

  ]
}
