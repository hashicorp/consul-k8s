# chart_dir returns the directory for the chart
chart_dir() {
    echo ${BATS_TEST_DIRNAME}/../..
}

# Usage: assert_empty helm template -s <template> [flags] .
# assert_empty makes it possible to test that a template is not rendered.
assert_empty() {
    run helm "${@:2}"
    [ "$status" -eq 1 ]
    [[ "$output" =~ "Error: could not find template" ]]
}
