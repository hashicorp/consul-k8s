// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package fetchserverregion

import (
	"os"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	cases := map[string]struct {
		args []string
		err  string
	}{
		"missing node name": {
			args: []string{},
			err:  "-node-name is required",
		},
		"missing output-file": {
			args: []string{"-node-name", "n1"},
			err:  "-output-file is required",
		},
	}

	for n, c := range cases {
		c := c
		t.Run(n, func(t *testing.T) {
			responseCode := cmd.Run(c.args)
			require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
			require.Contains(t, ui.ErrorWriter.String(), c.err)
		})
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		region      string
		expected    string
		missingNode bool
	}{
		"no region": {
			expected: `{"locality":{"region":""}}`,
		},
		"region": {
			region:   "us-east-1",
			expected: `{"locality":{"region":"us-east-1"}}`,
		},
		"missing node": {
			region:      "us-east-1",
			missingNode: true,
			expected:    `{"locality":{"region":""}}`,
		},
	}

	for n, c := range cases {
		c := c
		t.Run(n, func(t *testing.T) {
			outputFile, err := os.CreateTemp("", "ca")
			require.NoError(t, err)
			t.Cleanup(func() {
				os.RemoveAll(outputFile.Name())
			})

			var objs []runtime.Object
			if !c.missingNode {
				objs = append(objs, &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-node",
						Labels: map[string]string{
							corev1.LabelTopologyRegion: c.region,
						},
					},
				})
			}

			k8s := fake.NewSimpleClientset(objs...)
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}

			responseCode := cmd.Run([]string{
				"-node-name",
				"my-node",
				"-output-file",
				outputFile.Name(),
			})
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())
			require.NoError(t, err)
			cfg, err := os.ReadFile(outputFile.Name())
			require.NoError(t, err)
			require.Equal(t, c.expected, string(cfg))
		})
	}
}
