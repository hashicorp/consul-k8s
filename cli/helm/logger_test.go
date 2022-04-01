package helm

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common/terminal"
)

// TODO: finish this test by checking output
func TestCreateLogger(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		verbose bool
		msgs    []string
		expect  []string
	}{
		"verbose": {
			verbose: true,
			msgs: []string{
				"non verbose message",
				"not ready verbose message",
			},
			expect: []string{
				"non verbose message",
				"not ready verbose message",
			},
		},
		"not verbose": {
			verbose: false,
			msgs: []string{
				"non verbose message",
				"not ready verbose message",
			},
			expect: []string{
				"non verbose message",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ui := terminal.NewBasicUI(context.Background())

			logger := CreateLogger(ui, tc.verbose)

			for _, msg := range tc.msgs {
				logger(msg)
			}
		})
	}

}
