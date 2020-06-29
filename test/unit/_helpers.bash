# chart_dir returns the directory for the chart
chart_dir() {
    echo ${BATS_TEST_DIRNAME}/../..
}

# helm is used to intercept the `helm` command in tests and change flags depending
# on which version is being run.
# Helm 2 uses the -x flag instead of -s.
# NOTE: command is used so that this function isn't called recursively.
helm() {
  if [[ $(v2) ]]; then
    command helm template -x "${@:3}"
  else
    # The release name in Helm 3 defaults to RELEASE-NAME whereas it's lowercaes
    # in Helm 3 so we need to set it explictly.
    command helm template release-name -s "${@:3}"
  fi
}

# Usage: assert_empty helm template -s <template> [flags] .
# assert_empty makes it possible to test that a template is not rendered in
# both Helm 2 and 3.
assert_empty() {
  if [[ $(v2) ]]; then
      local actual=$(command helm template \
          -x "${@:4}"  \
          . | tee /dev/stderr |
          yq 'length > 0' | tee /dev/stderr)
    [ "${actual}" = "false" ]
  else
      run command helm "${@:2}"
      [ "$status" -eq 1 ]
      [[ "$output" =~ "Error: could not find template" ]]
  fi
}

# v2 outputs "1" if running Helm 2, otherwise it outputs nothing.
v2() {
  if [[ $(command helm version --short -c) =~ "v2" ]]; then
    echo 1
  fi
}
