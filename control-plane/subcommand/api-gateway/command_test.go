// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestRun_CommandRunsHappyPath(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp("", "")
	require.NoError(t, err)
	defer os.RemoveAll(f.Name())

	cases := []struct {
		flags []string
	}{
		{
			flags: nil,
		},
	}

	for _, c := range cases {
		t.Run("API Gateway Not Implemented", func(tt *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			exitCode := cmd.Run(c.flags)
			require.Equal(tt, 0, exitCode, ui.ErrorWriter.String())
		})
	}
}
