// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"io"

	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
)

// BaseCommand is embedded in all commands to provide common logic and data.
type BaseCommand struct {
	// Ctx is the base context for the command. It is up to commands to
	// utilize this context so that cancellation works in a timely manner.
	Ctx context.Context

	// Log is the logger to use.
	Log hclog.Logger

	// UI is used to write to the CLI.
	UI terminal.UI

	// CleanupConfirmation is the channel to update if cleanup is required and send confirmation that cleanup is done to main
	CleanupReqAndCompleted chan bool
}

// Close cleans up any resources that the command created. This should be
// defered by any CLI command that embeds baseCommand in the Run command.
func (c *BaseCommand) Close() error {
	// Close our UI if it implements it. The glint-based UI does for example
	// to finish up all the CLI output.
	var err error
	if closer, ok := c.UI.(io.Closer); ok && closer != nil {
		err = closer.Close()
	}
	if err != nil {
		return err
	}

	return nil
}
